# Sigbench

A tool to benchmark SignalR.

## Prerequisites

* Install Golang

## Build

```bash
mkdir -p sigbench/src
git clone https://github.com/ArchangelSDY/Sigbench sigbench/src/microsoft.com/sigbench
cd sigbench/src/microsoft.com/sigbench
make build
```

> If you are buidling on Windows, you can do a cross compilation by setting environment variables `GOOS=linux` and `GOARCH=amd64`.

## Run

Run as agent:

```bash
./sigbench
```

Run as master:

```bash
./sigbench -mode "cli" -outDir "output" -agents "172.0.0.2:7000,172.0.0.3:7000" -config "config.json"
```

This will run the benchmark defined in `config.json` using two agents at `172.0.0.2` and `172.0.0.3`. Intermediate data will be written to the `output` directory.

## Config

Here is a skeleton of config file:

```txt
{
    "Phases":[
        {
            "Name":"foobar",                    // Phase name. Each agent executes all phases sequentially.
            "UsersPerSecond":20,                // Number of users launched per second.
            "Duration":"1m"                     // Duration of the phase.
        }
    ],
    "SessionNames":[                            // Session names used by each user. Session name is defined in `sessions/session.go`. Each user executes only one session. Different user can execute different session and the percentage is controlled by the following `SessionPercentages`.
        "signalrcore:broadcast:sender"
    ],
    "SessionPercentages":[                      // Percentage of each session ranging from 0 to 1.
        1
    ],
    "SessionParams":{                           // Session parameters which will be passed to session in user context. Their meanings are defined in the code.
        "host": "172.17.4.17:5000",
        "broadcastDurationSecs": "60"
    }
}

```

Here is an example for SignalR .Net Core Broadcast:

```json
{
    "Phases":[
        {
            "Name":"broadcast",
            "UsersPerSecond":20,
            "Duration":"10s"
        }
    ],
    "SessionNames":[
        "signalrcore:broadcast:sender"
    ],
    "SessionPercentages":[
        1
    ],
    "SessionParams":{
        "host": "172.17.4.17:5000,172.17.4.18:5000",
        "broadcastDurationSecs": "60"
    }
}
```

It will:

1. Create users in the first 10 seconds at a rate of 20users/sec.
2. For each user, it will broadcast for 60 seconds.

### Session params

#### HTTP

* url
* maxConnsPerHost
* maxIdleConns
* maxIdleConnsPerHost
* keepAlive: true/false. Default is true
* tls: true/false. Default is false
* insecure: true/false. Default is false
* tlsClientSessionCache: Cache size
* proxy: proxy url
* quic: true/false. Default is false
* quicMaxStreams
* quicMaxConnections
* httpProxy: true/false
* proxyUser
* h2: true/false. Default is true

#### Kafka

* url
* topic
* messageSize
* messageCount
* partition: Number of partitions

#### Pulsar massive queue

* url
* messageSize
* queues
* queueMessages

#### RabbitMQ massive queue

* url
* messageSize
* queues
* queueMessages
* queueType
* deliveryMode

#### Redis Pub Sub

* host
* password
* broadcastDurationSecs
* publishInterval

#### SignalR .NET Core Broadcast

* host
* broadcastDurationSecs

#### SignalR .NET Core Echo

* host

#### SignalR .NET Framework Broadcast

* host
* broadcastDurationSecs

#### Web PubSub Echo

* url

## Develop

All benchmark scenarios are defined as sessions. Follow these steps if you want to add a new kind of scenario:

1. Add a new session under `sessions` package.
2. Register it in the `SessionMap` of `sessions/session.go`.
