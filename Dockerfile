FROM brimstone/ubuntu:14.04

MAINTAINER brimstone@the.narro.ws

# TORUN -v /var/run/docker.sock:/var/run/docker.sock

ENV GOPATH /go

# Set our command
ENTRYPOINT ["/go/bin/consuldock"]

# Install the packages we need, clean up after them and us
RUN apt-get update \
    && apt-get install -y --no-install-recommends git golang ca-certificates \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists

# Get and build our package
RUN go get github.com/brimstone/consuldock
