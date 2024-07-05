**Load Balancer for Multiple IP Addresses**
=============================================

This package provides a load balancer for multiple IP addresses, allowing you to distribute incoming requests across multiple servers. The load balancer selects the most reliable IP address based on failure rate and latency.

**Features**
------------

* Supports multiple IP addresses for a single hostname
* Calculates failure rate and average latency for each IP address
* Selects the most reliable IP address for each request
* Supports concurrent requests and updates
* Configurable bucket count and interval for statistics

**Usage**
-----

### Creating a new Manager instance

To create a new `Manager` instance, use the `New` function:
```go
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
			// TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
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

```

### Configuring the load balancer

You can configure the load balancer by setting the `bucketCount` and `interval` parameters when creating a new `Manager` instance. The `bucketCount` determines the number of buckets used to store statistics, and the `interval` determines how often the statistics are updated.

**Statistics**
-------------

The load balancer collects statistics for each IP address, including:

* Failure rate
* Average latency
* Success count
* Failure count

You can access these statistics using the `MostReliableIP` method, which returns the most reliable IP address and its statistics.

**License**
-------

This package is licensed under the MIT License.

**Contributing**
------------

Contributions are welcome! Please open a pull request or issue on GitHub.
