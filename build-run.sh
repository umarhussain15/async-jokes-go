#!/usr/bin/env bash

docker build -t jokes-app .

docker run -it --rm --net=host --name jokes-app jokes-app --uri "amqp://guest:guest@localhost:5672/"