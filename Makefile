VERSION=0.0.4

all: mackerel-plugin-memcached-lite

.PHONY: mackerel-plugin-memcached-lite

gom:
	go get -u github.com/mattn/gom

bundle:
	gom install

mackerel-plugin-memcached-lite: mackerel-plugin-memcached-lite.go
	gom build -o mackerel-plugin-memcached-lite

linux: mackerel-plugin-memcached-lite.go
	GOOS=linux GOARCH=amd64 gom build -o mackerel-plugin-memcached-lite

fmt:
	go fmt ./...

dist:
	git archive --format tgz HEAD -o mackerel-plugin-memcached-lite-$(VERSION).tar.gz --prefix mackerel-plugin-memcached-lite-$(VERSION)/

clean:
	rm -rf mackerel-plugin-memcached-lite mackerel-plugin-memcached-lite-*.tar.gz

