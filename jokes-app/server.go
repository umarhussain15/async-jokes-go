package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/icelain/jokeapi"
	amqp "github.com/rabbitmq/amqp091-go"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// flag parameters, which are read from cli arguments, otherwise given default values
var (
	uri            = flag.String("uri", "amqp://guest:guest@localhost:5672/", "AMQP URI")
	exchangeName   = flag.String("exchange", "test.go.example", "Durable AMQP exchange name")
	exchangeType   = flag.String("exchange-type", "direct", "Exchange type - direct|fanout|topic|x-custom")
	routingKey     = flag.String("routing-key", "joke-response", "AMQP routing key for publishing key")
	bindingKey     = flag.String("binding-key", "joke-request", "AMQP binding key for incoming requests")
	consumerTag    = flag.String("consumer-tag", "simple-consumer", "AMQP consumer tag (should not be blank)")
	reliable       = flag.Bool("reliable", true, "Wait for the publisher confirmation before exiting")
	subscribeQueue = flag.String("subscribe-queue", "joke-subscribe-queue", "Ephemeral AMQP subscribeQueue name")
	publishQueue   = flag.String("publish-queue", "joke-publish-queue", "Ephemeral AMQP subscribeQueue name")
	autoAck        = flag.Bool("auto_ack", false, "enable message auto-ack")
	workerId       = flag.String("worker-id", strconv.FormatInt(time.Now().Unix(), 10), "Unique id of the worker to identify it later")
	ErrLog         = log.New(os.Stderr, "[ERROR] ", log.LstdFlags|log.Lmsgprefix)
	Log            = log.New(os.Stdout, "[INFO] ", log.LstdFlags|log.Lmsgprefix)
)

var consumer Consumer
var producer Producer
var api *jokeapi.JokeAPI
var categories []string

// init go init function called on program startup
func init() {
	flag.Parse()
}

func main() {
	categories = []string{
		"programming",
		"dark",
		"miscellaneous",
		"pun",
		"spooky",
		"christmas",
	}
	api = jokeapi.New()
	api.Set(jokeapi.Params{
		Blacklist: []string{"nsfw", "religious", "political", "racist", "sexist", "explicit"},
	})
	// establish connection for the queue where the program will publish
	if err := setupPublishChannel(); err != nil {
		ErrLog.Fatalf("%s", err)
	}
	// establish another connection for the queue where the program will subscribe
	if err := setupSubscribeChannel(); err != nil {
		ErrLog.Fatalf("%s", err)
	}

	// handle os signals to gracefully terminate the connections to RabbitMQ
	SetupCloseHandlerAndWait(&consumer, &producer)
}

func SetupCloseHandlerAndWait(consumer *Consumer, producer *Producer) {
	//make channel to receive os signals
	c := make(chan os.Signal)
	//register for signals of interrupt and termination only
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	//start go routine to listen the signals
	go func() {
		// wait for a signal on the channel
		<-c
		Log.Printf("Ctrl+C pressed in Terminal")
		// process shutdown of the subscribe queue first
		if err := consumer.Shutdown(); err != nil {
			ErrLog.Printf("error during consumer shutdown: %s", err)
		}
		// process shutdown of publish queue
		if err := producer.Shutdown(); err != nil {
			ErrLog.Printf("error during producer shutdown: %s", err)
		}
	}()

	// wait for producer to finish/close in main routine
	<-producer.done
}

// setupPublishChannel tries to establish a connection to RabbitMQ server, open a channel, declare exchange, declare a
// queue to publish, bind that queue to the exchange with the given routing key and finally starts listening for
// confirmations of the published messages from the server. It will return error in case any of these operations fail
func setupPublishChannel() error {
	amqpURI := *uri

	// init publish struct object
	producer = Producer{
		conn:    nil,
		channel: nil,
		done:    make(chan bool),
		tag:     "",
	}

	Log.Printf("dialing publish connection %q", amqpURI)
	connectionPublish, err := amqp.Dial(amqpURI)
	if err != nil {
		return fmt.Errorf("dial publish: %s", err)
	}

	producer.conn = connectionPublish
	Log.Printf("got publish connection, getting Channel")
	producer.channel, err = connectionPublish.Channel()
	if err != nil {
		return fmt.Errorf("channelPublish: %s", err)
	}
	Log.Printf("got publish channel, declaring %q Exchange (%q)", *exchangeType, *exchangeName)
	// creates exchange if not present already
	if err := producer.channel.ExchangeDeclare(
		*exchangeName, // name
		*exchangeType, // type
		true,          // durable
		false,         // auto-deleted
		false,         // internal
		false,         // noWait
		nil,           // arguments
	); err != nil {
		return fmt.Errorf("exchange declare in publish: %s", err)
	}

	Log.Printf("got exchange, declaring publish Queue %q", *publishQueue)
	// creates a new queue if not present already
	if producer.queue, err = producer.channel.QueueDeclare(
		*publishQueue, // name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	); err != nil {
		return fmt.Errorf("publish queue declare: %s", err)
	}

	Log.Printf("got queue, binding publish queue (%q) to exchange (%q) with routing key (%q)", *publishQueue, *exchangeName, *routingKey)
	if err = producer.channel.QueueBind(
		producer.queue.Name, // name of the queue
		*routingKey,         // bindingKey
		*exchangeName,       // sourceExchange
		false,               // noWait
		nil,                 // arguments
	); err != nil {
		return fmt.Errorf("subscribe queue bind: %s", err)
	}

	// to receive confirmations from the server about the published messages from us
	var publishes chan uint64 = nil
	var confirms chan amqp.Confirmation = nil

	Log.Printf("enabling publisher confirms.")
	// Reliable publisher confirms require confirm.select support from the
	// connection.
	if err := producer.channel.Confirm(false); err != nil {
		return fmt.Errorf("channel could not be put into confirm mode: %s", err)
	}
	// We'll allow for a few outstanding publisher confirms
	publishes = make(chan uint64, 8)
	confirms = producer.channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	// start a go routine to receive confirmations asynchronously
	go confirmHandler(producer.done, publishes, confirms)

	return nil
}

func setupSubscribeChannel() error {
	amqpURI := *uri
	Log.Printf("dialing subscribe connection %q", amqpURI)
	connectionSubscribe, err := amqp.Dial(amqpURI)
	if err != nil {
		return fmt.Errorf("dial subscribe: %s", err)
	}
	Log.Printf("got subscribe Connection, getting Channel")
	channelSubscribe, err := connectionSubscribe.Channel()
	if err != nil {
		return fmt.Errorf("channelSubscribe: %s", err)
	}

	Log.Printf("got subscribe channel, declaring %q Exchange (%q)", *exchangeType, *exchangeName)
	// creates exchange if not present already
	if err := channelSubscribe.ExchangeDeclare(
		*exchangeName, // name
		*exchangeType, // type
		true,          // durable
		false,         // auto-deleted
		false,         // internal
		false,         // noWait
		nil,           // arguments
	); err != nil {
		return fmt.Errorf("exchange declare in subscribe: %s", err)
	}

	consumer = Consumer{
		conn:    connectionSubscribe,
		channel: channelSubscribe,
		tag:     *consumerTag,
		done:    make(chan error),
	}

	Log.Printf("got exchange, Declaring subscribe Queue %q", *subscribeQueue)
	// creates a new queue if not present already
	incomingQueue, err := channelSubscribe.QueueDeclare(
		*subscribeQueue, // name of the subscribeQueue
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // noWait
		nil,             // arguments
	)
	if err != nil {
		return fmt.Errorf("subscribe Queue declare: %s", err)
	}

	Log.Printf("declared subscribe queue (%q)[%d messages, %d consumers], binding to Exchange (%q) with key (%q)",
		incomingQueue.Name, incomingQueue.Messages, incomingQueue.Consumers, *exchangeName, *bindingKey)
	if err = channelSubscribe.QueueBind(
		incomingQueue.Name, // name of the queue
		*bindingKey,        // bindingKey
		*exchangeName,      // sourceExchange
		false,              // noWait
		nil,                // arguments
	); err != nil {
		return fmt.Errorf("subscribe queue bind: %s", err)
	}

	Log.Printf("Queue bound to Exchange, starting Consume (consumer tag %q)", consumer.tag)
	// start consuming subscribe channel queue, by getting the deliveries channel (go channel)
	deliveries, err := channelSubscribe.Consume(
		incomingQueue.Name, // name
		consumer.tag,       // consumerTag,
		*autoAck,           // autoAck
		false,              // exclusive
		false,              // noLocal
		false,              // noWait
		nil,                // arguments
	)
	if err != nil {
		return fmt.Errorf("subscribe queue consume: %s", err)
	}

	// start go routine to listen on the deliveries channel for the queued messages
	go receiveJokeRequest(deliveries, consumer.done)
	return nil
}

// receiveJokeRequest iterates over the amqp.Delivery channel to get messages from the queue and process them. It will
// keep running the loop infinitely in the separate routine until the done channel in the main function is empty.
// The done channel is set to be triggered by os.Signal received by the application
func receiveJokeRequest(deliveries <-chan amqp.Delivery, done chan error) {
	cleanup := func() {
		Log.Printf("receiveJokeRequest: subscribe channel closed")
		done <- nil
	}

	// when the function ends call this function
	defer cleanup()
	// iterate over channel, each item gives a message taken out from the queue
	for d := range deliveries {

		Log.Printf(
			"got %dB delivery: [%v][%q] headers %q body %q",
			len(d.Body),
			d.DeliveryTag,
			d.ContentType,
			d.Headers,
			d.Body,
		)
		// ack message so it is removed from queue. we need to do it if auto-ack is not set on queue declare
		// this is done here for the demo, but ideally, this can be done once the message is processed. Otherwise,
		// if we first ack the message and then the application crashes then we have unprocessed message, which is also
		// removed from the queue
		if *autoAck == false {
			if err := d.Ack(false); err != nil {
				_ = fmt.Errorf("failed to Ack Publish: %s", err)
			}
		}
		// start message processing based on the application logic.

		// if incoming message has content type set to json then process
		if d.ContentType == "application/json" {
			var message RequestMessage
			// unmarshal the body in the RequestMessage object
			err := json.Unmarshal(d.Body, &message)
			if err != nil {
				ErrLog.Printf("failed to unmarshall json body of request: %s", err)
				continue
			}
			// get the joke from the jokes api for the given parameters in the body of incoming message
			joke, err := getJoke(message)
			if err != nil {
				ErrLog.Printf("failed to get joke from api: %s", err)
				continue
			}
			// prepare bytes of the json data we want to publish on the response queue
			response, err := json.Marshal(joke)
			if err != nil {
				ErrLog.Printf("failed to marshal joke: %s", err)
				continue
			}
			//Log.Printf(string(response))
			//send the response to publish queue
			err = sendJokeResponse("application/json", response)
			if err != nil {
				ErrLog.Printf("failed to publish joke: %s", err)
			}
		} else {
			Log.Printf("message body is not json. ignoring the message")
		}
	}

}

// sendJokeResponse publishes the given bytes and the content-type string to publish queue.
func sendJokeResponse(ContentType string, body []byte) error {
	if err := producer.channel.PublishWithContext(
		context.Background(), // go context to use for this request
		*exchangeName,        // publish to an exchange
		*routingKey,          // routing to 0 or more queues
		false,                // mandatory
		false,                // immediate
		amqp.Publishing{
			Headers:         amqp.Table{}, // headers key value pairs
			ContentType:     ContentType,  // set content-type property on the message
			ContentEncoding: "",
			Body:            body,           // set body of the message
			DeliveryMode:    amqp.Transient, // 1=non-persistent, 2=persistent
			Priority:        0,              // 0-9
			// a bunch of application/implementation-specific fields
		},
	); err != nil {
		return fmt.Errorf("publish to queue: %s", err)
	}
	Log.Printf("sendJokeResponse published %dB OK", len(body))
	return nil
}

// getJoke calls the jokes api with the given RequestMessage parameters and generates a ResponseMessage if call
// is successful.
func getJoke(message RequestMessage) (joke ResponseMessage, err error) {

	api.SetCategories(message.Categories)
	api.SetLang(message.Lang)
	fetch, err := api.Fetch()
	if err != nil {
		return joke, err
	}
	if fetch.Error {
		return joke, errors.New("error fetching joke")
	}
	jsonString := strings.Join(fetch.Joke, "\n")
	response := ResponseMessage{
		fetch,
		*workerId,
		jsonString,
		"https://jokeapi.dev/",
	}
	return response, nil
}

// confirmHandler wait for the confirmations of the messages published to the server. It runs in a separate co-routine
// to work async and close the handler when it receives the signal on the given channel
func confirmHandler(done chan bool, publishes chan uint64, confirms chan amqp.Confirmation) {
	m := make(map[uint64]bool)
	for { // infinite for loop
		select { // select to wait on multiple channels and receive data
		case <-done: // receive message from done channel to stop listening/end the for loop
			Log.Println("confirmHandler is stopping")
			return
		case publishSeqNo := <-publishes: // listen on publishes channel
			Log.Printf("waiting for confirmation of %d", publishSeqNo)
			m[publishSeqNo] = false
		case confirmed := <-confirms: // listen on confirmation channel of RabbitMq
			if confirmed.DeliveryTag > 0 {
				if confirmed.Ack {
					Log.Printf("confirmed delivery with delivery tag: %d %d", confirmed.DeliveryTag, confirmed.Ack)
				} else {
					ErrLog.Printf("failed delivery of delivery tag: %d", confirmed.DeliveryTag)
				}
				delete(m, confirmed.DeliveryTag)
			}
		}
		if len(m) > 1 {
			Log.Printf("outstanding confirmations: %d", len(m))
		}
	}
}
