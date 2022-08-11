#!/usr/bin/env bash

docker build -t jokes-app .

docker network create rabbit-mq-demo 2>/dev/null

docker run -it --rm --net=rabbit-mq-demo --name jokes-app jokes-app --uri "amqp://guest:guest@rabbitmq:5672/"