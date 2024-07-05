// Package ipman provides a load balancer for multiple IP addresses
package ipman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Stats represents statistics for an IP address
type Stats struct {
	LatencySum time.Duration
	Success    int
	Fail       int
}

// failRate calculates the failure rate for an IP address
func (mgr *Stats) failRate() (failPercentage float64) {
	t := mgr.Success + mgr.Fail
	if t != 0 {
		failPercentage = float64(100*mgr.Fail) / float64(t)
	}
	return
}

// AverageLatency calculates the average latency for an IP address
func (mgr *Stats) AverageLatency() time.Duration {
	if mgr.Success == 0 {
		return 0
	}
	return mgr.LatencySum / time.Duration(mgr.Success)
}

// IPStats represents statistics for an IP address with buckets
type IPStats struct {
	Stats
	Buckets []Stats
}

// MostReliableIP returns the most reliable IP address based on failure rate and latency
func (mgr *Manager) MostReliableIP() string {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	var bestIP string
	var bestPercentage float64
	var bestLatency time.Duration
	for ip, stats := range mgr.ips {
		failPercentage := stats.failRate()
		aveLatency := stats.AverageLatency()

		fmt.Println(ip, failPercentage, aveLatency)
		if bestIP == "" || failPercentage < bestPercentage || aveLatency > 0 && aveLatency < bestLatency && math.Abs(failPercentage-bestPercentage) < 1 {
			bestIP = ip
			bestPercentage = failPercentage
			bestLatency = aveLatency
		}
	}

	return bestIP
}

// Manager represents a load balancer for multiple IP addresses
type Manager struct {
	bucketCount      int
	client           *http.Client
	hostname, Scheme string
	ips              map[string]*IPStats
	mu               sync.RWMutex
	clean, active    int
	start            time.Time
	tcpCheckTimeout  time.Duration
}

// New creates a new Manager instance
func New(URL string, bucketCount int, tickInterval, tcpCheckTimeout time.Duration, client *http.Client) (mgr *Manager, err error) {
	if bucketCount < 2 {
		return nil, errors.New("min bucket count is 2")
	}
	parsedURL, err := url.Parse(URL)
	if err != nil {
		return
	}
	hostname := parsedURL.Hostname()
	Scheme := parsedURL.Scheme
	if hostname == "" || Scheme == "" {
		return nil, errors.New("no hostname")
	}

	mgr = &Manager{
		bucketCount: bucketCount,
		client:      client,
		hostname:    hostname,
		Scheme:      Scheme,
		ips:         make(map[string]*IPStats),
		start:       time.Now(),
	}

	mgr.updateIPList()

	go mgr.tick(tickInterval)

	return mgr, nil
}

// DoRequest sends an HTTP request to the most reliable IP address
func (mgr *Manager) DoRequest(ctx context.Context, method, URL string, body io.Reader, headers map[string]string) (response *http.Response, err error) {
	if !strings.Contains(URL, mgr.hostname) {
		return nil, errors.New("hostname not found: " + mgr.hostname)
	}

	ip := mgr.MostReliableIP()
	if ip == "" {
		return nil, errors.New("no ip for the hostname: " + mgr.hostname)
	}

	URL = strings.Replace(URL, mgr.hostname, ip, 1)

	r, err := http.NewRequestWithContext(ctx, method, URL, body)
	if err != nil {
		return
	}

	for k, v := range headers {
		r.Header.Add(k, v)
	}

	var t0 = time.Now()
	response, err = mgr.client.Do(r)
	d := time.Since(t0)
	if err != nil {
		mgr.UpdateFailed(ip)
	} else {
		mgr.UpdateSuccess(ip, d)
	}
	return
}

// updateIPList updates the list of IP addresses
func (mgr *Manager) updateIPList() {
	ips, err := ResolveIPs(mgr.hostname, 2*time.Second)
	if err != nil {
		slog.Error("IPman", "hostname", mgr.hostname, "resolveIPs", err)
		return
	}
	if len(ips) == 0 {
		slog.Error("IPman", "hostname", mgr.hostname, "resolveIPs", "none")
		return
	}

	var removeDiscontinuedIps []string
	for ip := range mgr.ips {
		if !ips[ip] {
			removeDiscontinuedIps = append(removeDiscontinuedIps, ip)
		}
	}
	mgr.mu.Lock()
	for _, ip := range removeDiscontinuedIps {
		delete(mgr.ips, ip)
	}
	mgr.mu.Unlock()

	for ip := range ips {
		mgr.mu.Lock()
		if mgr.ips[ip] == nil {
			mgr.ips[ip] = &IPStats{Buckets: make([]Stats, mgr.bucketCount)}
		}
		mgr.mu.Unlock()
		go func() {
			adr := ip + ":" + mgr.Scheme
			err := CheckTCP(adr, mgr.tcpCheckTimeout)
			if err != nil {
				mgr.UpdateFailed(ip)
				slog.Error("IPman_check", "tcp", adr, "result", err)
			} else {
				slog.Info("IPman_check", "tcp", adr, "result", "OK")
			}
		}()
	}
}

// tick updates the IP list and logs statistics
func (mgr *Manager) tick(interval time.Duration) {
	for range time.Tick(interval) {
		mgr.updateIPList()

		mgr.mu.Lock()
		if len(mgr.ips) == 0 {
			mgr.mu.Unlock()
			continue
		}

		for ip, m := range mgr.ips {
			slog.Info("IPman_tick", "ip", ip, "Latency", m.AverageLatency(), "Bucket", mgr.active, "BktLatency", m.Buckets[mgr.active].AverageLatency(), "Success", m.Buckets[mgr.active].Success, "Fail", m.Buckets[mgr.active].Fail)
		}

		mgr.active = (mgr.active + 1) % mgr.bucketCount
		if mgr.active == mgr.clean {
			for _, ip := range mgr.ips {
				ip.LatencySum -= ip.Buckets[mgr.clean].LatencySum
				ip.Success -= ip.Buckets[mgr.clean].Success
				ip.Fail -= ip.Buckets[mgr.clean].Fail
				ip.Buckets[mgr.clean].LatencySum = 0
				ip.Buckets[mgr.clean].Success = 0
				ip.Buckets[mgr.clean].Fail = 0
			}
			mgr.clean = (mgr.clean + 1) % mgr.bucketCount
		}
		mgr.mu.Unlock()
	}
}

// UpdateSuccess updates the statistics for a successful request
func (mgr *Manager) UpdateSuccess(ip string, latency time.Duration) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	stats := mgr.ips[ip]

	stats.LatencySum += latency
	stats.Success++

	stats.Buckets[mgr.active].LatencySum += latency
	stats.Buckets[mgr.active].Success++
}

// UpdateFailed updates the statistics for a failed request
func (mgr *Manager) UpdateFailed(ip string) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	stats := mgr.ips[ip]

	stats.Fail++

	stats.Buckets[mgr.active].Fail++
}

// ResolveIPs resolves IP addresses for a hostname
func ResolveIPs(hostname string, timeout time.Duration) (newIPs map[string]bool, err error) {
	newIPs = map[string]bool{}
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		slog.Error("IPman", "LookupIP", err)
	} else {
		for _, ip := range ips {
			if ip.To16() == nil {
				continue
			}
			newIPs[ip.String()] = true
		}
		if len(newIPs) > 0 {
			return
		}
	}

	ctx2, cancel2 := context.WithTimeout(ctx, timeout)
	defer cancel2()

	ipst, err := net.DefaultResolver.LookupHost(ctx2, hostname)
	if err != nil {
		return
	}

	for _, ip := range ipst {
		newIPs[ip] = true
	}
	return newIPs, nil
}

// CheckTCP checks if a TCP connection can be established to an IP address
func CheckTCP(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
