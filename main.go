package main

import (
    "fmt"
    "./shim"
)

func main() {
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
    fmt.Println(err.Error())
}
