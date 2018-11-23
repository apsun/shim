#!/bin/sh
PORT=4153
echo 1 > /proc/sys/net/ipv4/ip_forward
iptables -t nat -A PREROUTING -p tcp --destination-port 80 -j REDIRECT --to-port $PORT
go run main.go $PORT
iptables -t nat -D PREROUTING -p tcp --destination-port 80 -j REDIRECT --to-port $PORT
echo 0 > /proc/sys/net/ipv4/ip_forward
