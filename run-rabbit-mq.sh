#!/usr/bin/env bash

docker network create rabbit-mq-demo  2>/dev/null

docker run -it --rm --net=rabbit-mq-demo  --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3.10-management