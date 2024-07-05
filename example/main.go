package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/wasmup/ipman"
)

func main() {

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        150,
			MaxIdleConnsPerHost: 25,
			MaxConnsPerHost:     250,
			IdleConnTimeout:     45 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		},
	}

	u := "https://example.com/"

	m, err := ipman.New(u, 2, 1*time.Second, 1*time.Second, client)
	if err != nil {
		fmt.Println(err)
		return
	}

	for i := 0; i < 10; i++ {
		fmt.Println(m.MostReliableIP())
		time.Sleep(1 * time.Second)
	}

}
