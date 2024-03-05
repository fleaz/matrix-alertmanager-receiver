# matrix-alertmanager-receiver

Simple daemon for forwarding
[prometheus-alertmanager](https://duckduckgo.com/?q=prometheus+alertmanagaer&ia=software)
events to a matrix room.

## Build

Make sure you have [Go](https://golang.org/) installed (`golang-bin` package on Fedora).

```
go build -v
```

## Container workflow

Container-related logic lives under the `contrib/` directory. One can build a
'docker' container using something along the lines of:

```
docker build -t matrix-alermanager-receiver:latest -f contrib/Dockerfile .
```

## Usage

There is no authentication build in. You are supposed to expose this service
via a proxy such as Nginx, providing basic HTTP authentication, or bind it only
on localhost on the same machine where your alertmanager is running.