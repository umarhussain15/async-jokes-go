package main

import (
	"fmt"
	"github.com/icelain/jokeapi"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	tag     string
	done    chan error
}

type Producer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
	tag     string
	done    chan bool
}

type RequestMessage struct {
	Format     string   `json:"format"`
	Lang       string   `json:"lang"`
	Categories []string `json:"categories"`
}

type ResponseMessage struct {
	jokeapi.JokesResp
	Worker     string `json:"worker"`
	JokeString string `json:"jokeString"`
	JokeSource string `json:"jokeSource"`
}

func (c *Consumer) Shutdown() error {
	// will close() the deliveries channelConsume
	if err := c.channel.Cancel(c.tag, true); err != nil {
		return fmt.Errorf("consumer cancel failed: %s", err)
	}

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}

	defer Log.Printf("AMQP consumer shutdown OK")

	// wait for receiveJokeRequest() to exit
	return <-c.done
}
func (p *Producer) Shutdown() error {
	// will close() the deliveries channelPublish
	if err := p.channel.Cancel(p.tag, true); err != nil {
		return fmt.Errorf("producer cancel failed: %s", err)
	}
	if err := p.conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}
	defer Log.Printf("AMQP producer shutdown OK")
	p.done <- true
	return nil
}
