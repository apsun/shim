# SHIM: Simple HTTP Interceptor (MyGodWhyAreYouNotUsingHTTPS)

This project is essentially arpspoof + sslstrip, but more customizable.
Write custom interception rules for requests. Inject JavaScript, flip the
page upside down, replace the font with Comic Sans, you name it!

But mainly, this project was just a way for me to learn Go. Unless you
want to write your own filter rules, there's no real reason to use this
over just arpspoof and sslstrip.

This project is a work in progress.

## Writing a custom filter

There are two stages of filtering: once for outgoing HTTP requests from
the client (before they are sent to the server), and once for incoming
HTTP responses from the server (before they are sent to the client).
Outgoing request filters should implement the `shim.RequestHandler`
interface, and incoming request filters should implement the
`shim.ResponseHandler` interface. Then, just instantiate your handler
and add it to the correct location in `main.go`.

Filters are applied in order, so you should generally make them nest
(i.e. if request handler order is A -> B, then response handler order
should be B -> A). It's perfectly valid to have a request handler with
no corresponding response handler, e.g. if you are just performing logging
and do not modify the request in any way.
