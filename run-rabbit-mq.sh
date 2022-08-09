#!/usr/bin/env bash

docker run -it --rm --net=host  --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3.10-management