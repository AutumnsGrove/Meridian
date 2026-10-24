package probe_test

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/autumnsgrove/meridian/internal/probe"
	"golang.org/x/net/dns/dnsmessage"
)

// dnsServer runs a tiny UDP DNS server on 127.0.0.1:0 and returns its address. answer builds the
// reply for one query, or returns nil to stay silent. It stops when the test ends.
func dnsServer(t *testing.T, answer func(query []byte) []byte) string {
	t.Helper()
	var listenConfig net.ListenConfig
	conn, err := listenConfig.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		for {
			buffer := make([]byte, 1500)
			size, from, readErr := conn.ReadFrom(buffer)
			if readErr != nil {
				return
			}
			if reply := answer(buffer[:size]); reply != nil {
				if _, writeErr := conn.WriteTo(reply, from); writeErr != nil {
					return
				}
			}
		}
	}()
	return conn.LocalAddr().String()
}

// parseQuery reads the id and question out of a client packet.
func parseQuery(t *testing.T, packet []byte) (dnsmessage.Header, dnsmessage.Question) {
	t.Helper()
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		t.Fatalf("parse query header: %v", err)
	}
	question, err := parser.Question()
	if err != nil {
		t.Fatalf("parse query question: %v", err)
	}
	return header, question
}

// replyWith builds a response to query carrying rcode and, when answered, one A record.
func replyWith(t *testing.T, query []byte, rcode dnsmessage.RCode, answered bool) []byte {
	t.Helper()
	header, question := parseQuery(t, query)
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID:                 header.ID,
		Response:           true,
		RecursionAvailable: true,
		RCode:              rcode,
	})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		t.Fatalf("start questions: %v", err)
	}
	if err := builder.Question(question); err != nil {
		t.Fatalf("question: %v", err)
	}
	if answered {
		if err := builder.StartAnswers(); err != nil {
			t.Fatalf("start answers: %v", err)
		}
		if err := builder.AResource(dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}); err != nil {
			t.Fatalf("answer: %v", err)
		}
	}
	packet, err := builder.Finish()
	if err != nil {
		t.Fatalf("finish reply: %v", err)
	}
	return packet
}

func resolve(t *testing.T, addr, qname string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return probe.NewDNSResolver()(ctx, addr, qname)
}

func TestDNSResolver_ReturnsTheResponseCode(t *testing.T) {
	noerror := dnsServer(t, func(query []byte) []byte {
		return replyWith(t, query, dnsmessage.RCodeSuccess, true)
	})
	rcode, err := resolve(t, noerror, "example.org.")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rcode != "NOERROR" {
		t.Errorf("rcode = %q, want NOERROR", rcode)
	}

	nameError := dnsServer(t, func(query []byte) []byte {
		return replyWith(t, query, dnsmessage.RCodeNameError, false)
	})
	rcode, err = resolve(t, nameError, "missing.example.org.")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rcode != "NXDOMAIN" {
		t.Errorf("rcode = %q, want NXDOMAIN", rcode)
	}
}

func TestDNSResolver_AcceptsANameWithoutATrailingDot(t *testing.T) {
	addr := dnsServer(t, func(query []byte) []byte {
		return replyWith(t, query, dnsmessage.RCodeSuccess, true)
	})
	if _, err := resolve(t, addr, "example.org"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

func TestDNSResolver_BadNameIsBadRequest(t *testing.T) {
	addr := dnsServer(t, func(query []byte) []byte {
		return replyWith(t, query, dnsmessage.RCodeSuccess, true)
	})
	longLabel := strings.Repeat("a", 64) + ".example.org."
	if _, err := resolve(t, addr, longLabel); !errors.Is(err, probe.ErrBadRequest) {
		t.Fatalf("err = %v, want %v", err, probe.ErrBadRequest)
	}
}

func TestDNSResolver_RefusesAnAnswerToAnotherQuestion(t *testing.T) {
	tests := []struct {
		name  string
		reply func(t *testing.T, query []byte) []byte
	}{
		{
			name: "another id",
			reply: func(t *testing.T, query []byte) []byte {
				reply := replyWith(t, query, dnsmessage.RCodeSuccess, true)
				binary.BigEndian.PutUint16(reply[0:2], binary.BigEndian.Uint16(reply[0:2])+1)
				return reply
			},
		},
		{
			name: "another name",
			reply: func(t *testing.T, query []byte) []byte {
				header, _ := parseQuery(t, query)
				other, err := dnsmessage.NewName("other.example.org.")
				if err != nil {
					t.Fatalf("name: %v", err)
				}
				builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true})
				if err := builder.StartQuestions(); err != nil {
					t.Fatalf("start questions: %v", err)
				}
				if err := builder.Question(dnsmessage.Question{Name: other, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}); err != nil {
					t.Fatalf("question: %v", err)
				}
				packet, err := builder.Finish()
				if err != nil {
					t.Fatalf("finish: %v", err)
				}
				return packet
			},
		},
		{
			name: "a short packet",
			reply: func(_ *testing.T, query []byte) []byte {
				packet := make([]byte, 3)
				copy(packet, query[:2])
				return packet
			},
		},
		{
			name: "garbage",
			reply: func(_ *testing.T, query []byte) []byte {
				packet := []byte("this is not a DNS packet, it is a sentence")
				copy(packet, query[:2])
				return packet
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addr := dnsServer(t, func(query []byte) []byte { return test.reply(t, query) })
			if _, err := resolve(t, addr, "example.org."); !errors.Is(err, probe.ErrUnreachable) {
				t.Fatalf("err = %v, want %v", err, probe.ErrUnreachable)
			}
		})
	}
}

func TestDNSResolver_SilentServerIsATimeout(t *testing.T) {
	addr := dnsServer(t, func([]byte) []byte { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := probe.NewDNSResolver()(ctx, addr, "example.org.")
	if !errors.Is(err, probe.ErrTimeout) {
		t.Fatalf("err = %v, want %v", err, probe.ErrTimeout)
	}
}

// FuzzDNSResponse feeds arbitrary bytes through the resolver as the server's reply, with the query
// id patched in so the parser is reached: no panic, and the result is an error or a response code.
func FuzzDNSResponse(f *testing.F) {
	for _, seed := range []string{
		"", "\x00", "\x00\x00\x81\x80\x00\x00\x00\x00\x00\x00\x00\x00",
		"garbage that is not a DNS packet at all",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, reply []byte) {
		var listenConfig net.ListenConfig
		conn, err := listenConfig.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer func() { _ = conn.Close() }()

		answered := make(chan struct{})
		go func() {
			defer close(answered)
			buffer := make([]byte, 1500)
			size, from, readErr := conn.ReadFrom(buffer)
			if readErr != nil {
				return
			}
			answer := make([]byte, len(reply))
			copy(answer, reply)
			if len(answer) >= 2 && size >= 2 {
				copy(answer[:2], buffer[:2]) // the id the resolver will check
			}
			_, _ = conn.WriteTo(answer, from)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rcode, resolveErr := probe.NewDNSResolver()(ctx, conn.LocalAddr().String(), "example.org.")
		<-answered

		if resolveErr != nil {
			for _, sentinel := range []error{probe.ErrUnreachable, probe.ErrTimeout, probe.ErrRefused, probe.ErrBadRequest} {
				if errors.Is(resolveErr, sentinel) {
					return
				}
			}
			t.Fatalf("error %v is not in the closed set", resolveErr)
		}
		if rcode == "" {
			t.Fatal("an answer with no response code")
		}
	})
}
