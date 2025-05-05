package main

import (
    "encoding/csv"
    "fmt"
    "html/template"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "github.com/gin-gonic/gin"
)

func main() {
    router := gin.Default()
    router.LoadHTMLGlob("views/*")
    router.Static("/static", "./static")

    router.GET("/", showIndex)
    router.POST("/upload-template", uploadTemplate)
    router.POST("/upload-csv", uploadCSV)
    router.POST("/send-emails", sendEmails)

    router.Run(":8080")
}
