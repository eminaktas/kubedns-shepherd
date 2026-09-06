/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type dnsCandidateResult struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Requests  uint64            `json:"requests"`
	Responses uint64            `json:"responses"`
	RCodes    map[string]uint64 `json:"rcodes"`
}

type result struct {
	Target            string               `json:"target"`
	Queries           int                  `json:"queries"`
	Concurrency       int                  `json:"concurrency"`
	Successes         uint64               `json:"successes"`
	Failures          uint64               `json:"failures"`
	DNSRequests       uint64               `json:"dns_requests"`
	DNSResponses      uint64               `json:"dns_responses"`
	NXDOMAINResponses uint64               `json:"nxdomain_responses"`
	Candidates        []dnsCandidateResult `json:"candidates"`
	DurationSeconds   float64              `json:"duration_seconds"`
	QueriesPerSecond  float64              `json:"queries_per_second"`
}

type dnsCandidateStats struct {
	requests  uint64
	responses uint64
	rcodes    map[string]uint64
}

type dnsTracker struct {
	mu         sync.Mutex
	requests   uint64
	responses  uint64
	nxdomain   uint64
	candidates map[string]*dnsCandidateStats
}

type trackingConn struct {
	net.Conn
	tracker   *dnsTracker
	tcpFramed bool
}

type trackingPacketConn struct {
	*trackingConn
	packetConn net.PacketConn
}

type dnsMessage struct {
	name     string
	qtype    string
	response bool
	rcode    string
}

func main() {
	target := envString("TARGET", "api.prod.external.test")
	queries := envPositiveInt("QUERIES", 250)
	concurrency := envPositiveInt("CONCURRENCY", 1)
	timeout := time.Duration(envPositiveInt("LOOKUP_TIMEOUT_SECONDS", 2)) * time.Second

	resolvConf, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read /etc/resolv.conf: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("--- /etc/resolv.conf ---")
	fmt.Print(string(resolvConf))
	fmt.Println("--- end /etc/resolv.conf ---")

	tracker := newDNSTracker()
	dialer := &net.Dialer{}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, dialErr := dialer.DialContext(ctx, network, address)
			if dialErr != nil {
				return nil, dialErr
			}

			tracked := &trackingConn{
				Conn:      conn,
				tracker:   tracker,
				tcpFramed: strings.HasPrefix(network, "tcp"),
			}
			if packetConn, ok := conn.(net.PacketConn); ok {
				return &trackingPacketConn{
					trackingConn: tracked,
					packetConn:   packetConn,
				}, nil
			}
			return tracked, nil
		},
	}
	jobs := make(chan struct{})
	var successes atomic.Uint64
	var failures atomic.Uint64

	started := time.Now()
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			for range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				_, lookupErr := resolver.LookupIP(ctx, "ip4", target)
				cancel()
				if lookupErr != nil {
					failures.Add(1)
					continue
				}
				successes.Add(1)
			}
		})
	}

	for range queries {
		jobs <- struct{}{}
	}
	close(jobs)
	workers.Wait()

	duration := time.Since(started)
	queriesPerSecond := float64(queries) / duration.Seconds()
	dnsRequests, dnsResponses, nxdomainResponses, candidates := tracker.snapshot()
	summary := result{
		Target:            target,
		Queries:           queries,
		Concurrency:       concurrency,
		Successes:         successes.Load(),
		Failures:          failures.Load(),
		DNSRequests:       dnsRequests,
		DNSResponses:      dnsResponses,
		NXDOMAINResponses: nxdomainResponses,
		Candidates:        candidates,
		DurationSeconds:   duration.Seconds(),
		QueriesPerSecond:  queriesPerSecond,
	}

	fmt.Println("--- result ---")
	if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		fmt.Fprintf(os.Stderr, "Could not encode result: %v\n", err)
		os.Exit(1)
	}

	if summary.Failures > 0 {
		os.Exit(1)
	}
}

func newDNSTracker() *dnsTracker {
	return &dnsTracker{candidates: make(map[string]*dnsCandidateStats)}
}

func (t *dnsTracker) recordRequest(packet []byte, tcpFramed bool) {
	message, ok := parseDNSMessage(packet, tcpFramed)
	if !ok || message.response {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests++
	candidate := t.candidate(message)
	candidate.requests++
}

func (t *dnsTracker) recordResponse(packet []byte, tcpFramed bool) {
	message, ok := parseDNSMessage(packet, tcpFramed)
	if !ok || !message.response {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.responses++
	if message.rcode == "NXDOMAIN" {
		t.nxdomain++
	}
	candidate := t.candidate(message)
	candidate.responses++
	candidate.rcodes[message.rcode]++
}

func (t *dnsTracker) candidate(message dnsMessage) *dnsCandidateStats {
	key := message.name + "\x00" + message.qtype
	candidate, ok := t.candidates[key]
	if !ok {
		candidate = &dnsCandidateStats{rcodes: make(map[string]uint64)}
		t.candidates[key] = candidate
	}
	return candidate
}

func (t *dnsTracker) snapshot() (uint64, uint64, uint64, []dnsCandidateResult) {
	t.mu.Lock()
	defer t.mu.Unlock()

	candidates := make([]dnsCandidateResult, 0, len(t.candidates))
	for key, stats := range t.candidates {
		parts := strings.SplitN(key, "\x00", 2)
		rcodes := make(map[string]uint64, len(stats.rcodes))
		maps.Copy(rcodes, stats.rcodes)
		candidates = append(candidates, dnsCandidateResult{
			Name:      parts[0],
			Type:      parts[1],
			Requests:  stats.requests,
			Responses: stats.responses,
			RCodes:    rcodes,
		})
	}
	slices.SortFunc(candidates, func(a, b dnsCandidateResult) int {
		if a.Name == b.Name {
			return strings.Compare(a.Type, b.Type)
		}
		return strings.Compare(a.Name, b.Name)
	})

	return t.requests, t.responses, t.nxdomain, candidates
}

func (c *trackingConn) Write(packet []byte) (int, error) {
	c.tracker.recordRequest(packet, c.tcpFramed)
	return c.Conn.Write(packet)
}

func (c *trackingConn) Read(packet []byte) (int, error) {
	n, err := c.Conn.Read(packet)
	if n > 0 {
		c.tracker.recordResponse(packet[:n], c.tcpFramed)
	}
	return n, err
}

func (c *trackingPacketConn) WriteTo(packet []byte, address net.Addr) (int, error) {
	c.tracker.recordRequest(packet, false)
	return c.packetConn.WriteTo(packet, address)
}

func (c *trackingPacketConn) ReadFrom(packet []byte) (int, net.Addr, error) {
	n, address, err := c.packetConn.ReadFrom(packet)
	if n > 0 {
		c.tracker.recordResponse(packet[:n], false)
	}
	return n, address, err
}

func parseDNSMessage(packet []byte, tcpFramed bool) (dnsMessage, bool) {
	if tcpFramed {
		if len(packet) < 2 {
			return dnsMessage{}, false
		}
		messageLength := int(binary.BigEndian.Uint16(packet[:2]))
		if messageLength > len(packet)-2 {
			return dnsMessage{}, false
		}
		packet = packet[2 : 2+messageLength]
	}
	if len(packet) < 12 || binary.BigEndian.Uint16(packet[4:6]) == 0 {
		return dnsMessage{}, false
	}

	name, offset, ok := decodeDNSName(packet, 12)
	if !ok || offset+4 > len(packet) {
		return dnsMessage{}, false
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	return dnsMessage{
		name:     name,
		qtype:    dnsTypeName(binary.BigEndian.Uint16(packet[offset : offset+2])),
		response: flags&0x8000 != 0,
		rcode:    dnsRCodeName(flags & 0x000f),
	}, true
}

func decodeDNSName(packet []byte, start int) (string, int, bool) {
	labels := make([]string, 0, 4)
	offset := start
	next := start
	jumped := false

	for range 128 {
		if offset >= len(packet) {
			return "", 0, false
		}
		length := int(packet[offset])
		switch {
		case length == 0:
			if !jumped {
				next = offset + 1
			}
			return strings.Join(labels, ".") + ".", next, true
		case length&0xc0 == 0xc0:
			if offset+1 >= len(packet) {
				return "", 0, false
			}
			if !jumped {
				next = offset + 2
			}
			offset = ((length & 0x3f) << 8) | int(packet[offset+1])
			jumped = true
		case length&0xc0 != 0 || offset+1+length > len(packet):
			return "", 0, false
		default:
			labels = append(labels, string(packet[offset+1:offset+1+length]))
			offset += 1 + length
			if !jumped {
				next = offset
			}
		}
	}
	return "", 0, false
}

func dnsTypeName(qtype uint16) string {
	switch qtype {
	case 1:
		return "A"
	case 28:
		return "AAAA"
	default:
		return strconv.FormatUint(uint64(qtype), 10)
	}
}

func dnsRCodeName(rcode uint16) string {
	switch rcode {
	case 0:
		return "NOERROR"
	case 1:
		return "FORMERR"
	case 2:
		return "SERVFAIL"
	case 3:
		return "NXDOMAIN"
	case 5:
		return "REFUSED"
	default:
		return strconv.FormatUint(uint64(rcode), 10)
	}
}

func envString(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envPositiveInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		fmt.Fprintf(os.Stderr, "%s must be a positive integer, got %q\n", name, value)
		os.Exit(2)
	}
	return parsed
}
