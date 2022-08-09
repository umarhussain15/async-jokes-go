# Jokes App

This repository demonstrates the usage of RabbitMQ server with Go application. The application subscribe to a queue
to receive request and then publishes the response on another queue.

The application calls the jokes api and use the result as response to send to publish queue.


## Async API
The `asyncapi.yaml` file contains the definitions of the api this application uses.  

## Run the application

* Run the script "run-rabbit-mq.sh" using `bash run-rabbit-mq.sh` to start a container of RabbitMQ with default settings
* Then in a separate terminal run `bash build-run.sh` to start the application container.
* Now the application is listening to the `joke-subscribe-queue` which is bound to exchange `test.go.example` with 
routing key `joke-request`.
* It will respond with a joke to the queue `joke-publish-queue` which is bound to the exchange `test.go.example` with 
routing key `joke-response`

## RabbitMQ management UI

To access the management UI of RabbitMQ you can go to the url: http://localhost:15672/ and login with default admin
credentials:
`username: guest` and `password: guest`

Here you can see the administration options and the list of exchanges, queues and access the messages in the queue.
You can also add new messages to the queues as well. 

## Sources

* [RabbitMQ Tutorials Github](https://github.com/rabbitmq/rabbitmq-tutorials)
* [RabbitMQ Get Started](https://www.rabbitmq.com/getstarted.html)
* [AsyncAPI](https://www.asyncapi.com)
