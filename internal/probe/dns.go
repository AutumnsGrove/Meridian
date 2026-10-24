package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// Resolver asks one DNS question and returns the response code name (NOERROR, NXDOMAIN, SERVFAIL…).
// It is the seam tests replace; the real one is a single UDP exchange.
type Resolver func(ctx context.Context, addr, qname string) (rcode string, err error)

// NewDNSResolver returns the real Resolver: one A question in one UDP packet, a random 16-bit id, and
// the reply checked against that id and the question. The truncation bit is ignored, because an
// answer is an answer for a health check. Callers set a context deadline; that is the only timeout.
func NewDNSResolver() Resolver {
	return func(ctx context.Context, addr, qname string) (string, error) {
		packet, id, err := buildDNSQuery(qname)
		if err != nil {
			return "", err
		}

		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "udp", addr)
		if err != nil {
			return "", classify(err)
		}
		defer func() { _ = conn.Close() }()
		// A cancelled context must not leave the read blocked, so closing the connection is tied to
		// it as well as to the deadline.
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()

		if _, err := conn.Write(packet); err != nil {
			return "", readError(ctx, err)
		}

		// One UDP answer fits in far less, but the read must not grow without a bound.
		buffer := make([]byte, 1500)
		size, err := conn.Read(buffer)
		if err != nil {
			return "", readError(ctx, err)
		}
		return parseDNSResponse(buffer[:size], id, qname)
	}
}

// buildDNSQuery packs an A question with a random id from crypto/rand.
func buildDNSQuery(qname string) (packet []byte, id uint16, err error) {
	name, err := dnsmessage.NewName(ensureDot(qname))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %q is not a DNS name", ErrBadRequest, qname)
	}
	var random [2]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, 0, fmt.Errorf("probe: random bytes for a DNS id: %w", err)
	}
	id = binary.BigEndian.Uint16(random[:])

	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		return nil, 0, fmt.Errorf("%w: build the DNS question: %w", ErrBadRequest, err)
	}
	if err := builder.Question(dnsmessage.Question{
		Name:  name,
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
	}); err != nil {
		return nil, 0, fmt.Errorf("%w: %q is not a DNS question: %w", ErrBadRequest, qname, err)
	}
	packet, err = builder.Finish()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: build the DNS query: %w", ErrBadRequest, err)
	}
	return packet, id, nil
}

// parseDNSResponse checks that packet answers the question we asked and returns its response code.
// A mismatched id, a truncated packet or garbage is ErrUnreachable: never a panic, never a guessed
// answer.
func parseDNSResponse(packet []byte, id uint16, qname string) (string, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		return "", fmt.Errorf("%w: the DNS reply is not a packet", ErrUnreachable)
	}
	if header.ID != id {
		return "", fmt.Errorf("%w: the DNS reply has another id", ErrUnreachable)
	}
	question, err := parser.Question()
	if err != nil {
		return "", fmt.Errorf("%w: the DNS reply has no question", ErrUnreachable)
	}
	if question.Type != dnsmessage.TypeA || !strings.EqualFold(question.Name.String(), ensureDot(qname)) {
		return "", fmt.Errorf("%w: the DNS reply answers another question", ErrUnreachable)
	}
	return rcodeName(header.RCode), nil
}

// The response codes dnsmessage does not name: they are only reachable through an extended code, and
// the config still needs the word for them.
const (
	rcodeYXDomain dnsmessage.RCode = 6
	rcodeYXRrset  dnsmessage.RCode = 7
	rcodeNXRrset  dnsmessage.RCode = 8
	rcodeNotAuth  dnsmessage.RCode = 9
	rcodeNotZone  dnsmessage.RCode = 10
)

// rcodeNames is the response code vocabulary the config uses, which is the name dig prints rather
// than dnsmessage's Go identifier.
var rcodeNames = map[dnsmessage.RCode]string{
	dnsmessage.RCodeSuccess:        "NOERROR",
	dnsmessage.RCodeFormatError:    "FORMERR",
	dnsmessage.RCodeServerFailure:  "SERVFAIL",
	dnsmessage.RCodeNameError:      "NXDOMAIN",
	dnsmessage.RCodeNotImplemented: "NOTIMP",
	dnsmessage.RCodeRefused:        "REFUSED",
	rcodeYXDomain:                  "YXDOMAIN",
	rcodeYXRrset:                   "YXRRSET",
	rcodeNXRrset:                   "NXRRSET",
	rcodeNotAuth:                   "NOTAUTH",
	rcodeNotZone:                   "NOTZONE",
}

// rcodeName returns the config's name for a response code. A code outside the table gets a name no
// config can expect, so it reads as dns_rcode instead of matching by accident.
func rcodeName(code dnsmessage.RCode) string {
	if name, ok := rcodeNames[code]; ok {
		return name
	}
	return fmt.Sprintf("RCODE%d", int(code))
}

// readError maps a read or write failure, preferring the context's own error so a cancelled probe
// reads as a timeout rather than as a closed socket.
func readError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return classify(ctxErr)
	}
	return classify(err)
}

// ensureDot gives a name the trailing dot dnsmessage prints, so names compare case-insensitively.
func ensureDot(qname string) string {
	if strings.HasSuffix(qname, ".") {
		return qname
	}
	return qname + "."
}
