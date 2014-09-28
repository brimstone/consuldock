# consuldock

Dynamic Consul Node/service creation based on docker containers

Uses the following libraries:

* [samalba/dockerclient](https://github.com/samalba/dockerclient)
* [armon/consul-api](https://github.com/armon/consul-api)

## Requirements
* [Docker](http://docker.io) v1.0.0 Maybe earlier is ok
* [consul](http://consul.io) v0.3.0

## Installation
Simply `go get github.com/brimstone/consuldock` or `docker run -d -v /var/run/docker.sock:/var/run/docker.sock brimstone/consuldock`

## Usage
Everything should happen automatically. Worst case, some cli flags can be passed:

* --consul: Address of consul server, if not on localhost or provided by a container named 'consul'
* --dockersock: Path to docker sock if not `/var/run/docker.sock`

