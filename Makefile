VERSION=0.0.4
TARGETS_NOVENDOR=$(shell glide novendor)

all: mackerel-plugin-memcached-lite

.PHONY: mackerel-plugin-memcached-lite

glide:
	go get -u github.com/Masterminds/glide

bundle:
	glide install

mackerel-plugin-memcached-lite: mackerel-plugin-memcached-lite.go
	GO15VENDOREXPERIMENT=1 go build mackerel-plugin-memcached-lite.go

linux: mackerel-plugin-memcached-lite.go
	GOOS=linux GOARCH=amd64 GO15VENDOREXPERIMENT=1 go build mackerel-plugin-memcached-lite.go

fmt:
	go fmt ./...

dist:
	git archive --format tgz HEAD -o mackerel-plugin-memcached-lite-$(VERSION).tar.gz --prefix mackerel-plugin-memcached-lite-$(VERSION)/

clean:
	rm -rf mackerel-plugin-memcached-lite mackerel-plugin-memcached-lite-*.tar.gz

