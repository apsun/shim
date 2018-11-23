package shim

import (
    "fmt"
    "io"
    "net/http"
    "net/url"
    "time"
)

type RequestHandler interface {
    // Handle an incoming HTTP request from the victim client.
    // Perform any modifications to the request as necessary,
    // then return to pass the request up the stack.
    OnRequest(req *http.Request) error
}

type ResponseHandler interface {
    // Handle an incoming HTTP response from the real server.
    // Perform any modifications to the response as necessary,
    // then return to pass the response down the stack.
    OnResponse(resp *http.Response) error
}

// The core framework used to modify requests and responses.
// The general architecture is as follows:
//
//      Server ------------> Response
//         ^                     |
//         |                     v
//        ...            ResponseHandlers[0]
//         |                     |
//  RequestHandlers[1]           v
//         ^             ResponseHandlers[1]
//         |                     |
//  RequestHandlers[0]          ...
//         ^                     |
//         |                     v
//      Request <------------ Client
//
type Server struct {
    RequestHandlers []RequestHandler
    ResponseHandlers []ResponseHandler
    client *http.Client
}

// Handler for requests from the client to us.
// Modifies the request, forwards the request to the server,
// modifies the response, forwards the response to the client.
func (s *Server) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
    // For some reason Go doesn't populate these fields for us,
    // let's just do it so the request handlers don't have to
    req.URL.Scheme = "http"
    req.URL.Host = req.Host

    // Filter request through handlers
    for _, handler := range s.RequestHandlers {
        err := handler.OnRequest(req)
        if err != nil {
            http.Error(resp, err.Error(), 500)
            return
        }
    }

    // Forward request to the real server
    req.RequestURI = ""
    proxyResp, err := s.client.Do(req)

    // If forwarded request failed, client receives an error.
    // Unfortunately we can't "return a timeout", so just
    // pray that client handles timeout == 504 error code.
    if err != nil {
        if err.(*url.Error).Timeout() {
            http.Error(resp, err.Error(), 504)
        } else {
            http.Error(resp, err.Error(), 500)
        }
        return
    }

    // Filter response through handlers
    for _, handler := range s.ResponseHandlers {
        err := handler.OnResponse(proxyResp)
        if err != nil {
            http.Error(resp, err.Error(), 500)
            return
        }
    }

    // Forward response to the client
    for name, values := range proxyResp.Header {
        for _, value := range values {
            resp.Header().Add(name, value)
        }
    }
    resp.WriteHeader(proxyResp.StatusCode)
    io.Copy(resp, proxyResp.Body)
}

// Runs the shim server on the specified port. Blocks
// until the server exits.
func (s *Server) Run(port uint16) error {
    s.client = &http.Client{
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse
        },
        Timeout: 10 * time.Second,
    }
    addr := fmt.Sprintf(":%d", port)
    return http.ListenAndServe(addr, s)
}
