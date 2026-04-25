package emercoin9p

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	dp9ik "github.com/kiljoy001/go-dp9ik"
	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/client"
)

const (
	maxP9AnyMessage = 4096
)

var (
	testAuthServerAddr = os.Getenv("DP9IK_TEST_AUTH_SERVER")
	testAuthDomain     = os.Getenv("DP9IK_TEST_AUTH_DOMAIN")
	testAuthUser       = os.Getenv("DP9IK_TEST_AUTH_USER")
	testAuthPassword   = os.Getenv("DP9IK_TEST_AUTH_PASSWORD")
)

func requireTestAuthServer(t *testing.T) {
	t.Helper()

	var missing []string
	if testAuthServerAddr == "" {
		missing = append(missing, "DP9IK_TEST_AUTH_SERVER")
	}
	if testAuthDomain == "" {
		missing = append(missing, "DP9IK_TEST_AUTH_DOMAIN")
	}
	if testAuthUser == "" {
		missing = append(missing, "DP9IK_TEST_AUTH_USER")
	}
	if testAuthPassword == "" {
		missing = append(missing, "DP9IK_TEST_AUTH_PASSWORD")
	}
	if len(missing) > 0 {
		t.Skipf("live auth tests require %s", strings.Join(missing, ", "))
	}

	conn, err := net.DialTimeout("tcp", testAuthServerAddr, 2*time.Second)
	if err != nil {
		t.Skipf("auth server unavailable: %v", err)
	}
	_ = conn.Close()
}

func openTestClientWithOptions(t *testing.T, ns *Namespace, user string, opts ...client.Option) (*client.Client, error) {
	t.Helper()

	p1r, p1w := io.Pipe()
	p2r, p2w := io.Pipe()
	pipe := &twoPipe{PipeReader: p2r, PipeWriter: p1w}

	go func() {
		_ = go9p.ServeReadWriter(p1r, p2w, ns.Server())
	}()

	c, err := client.NewClient(pipe, user, "", opts...)
	if err != nil {
		_ = pipe.Close()
		return nil, err
	}

	t.Cleanup(func() {
		_ = pipe.Close()
	})

	return c, nil
}

func newDP9IKTestClientAuth(authServer, authDomain, serverUser, password string) func(string, io.ReadWriter) (string, error) {
	return func(user string, s io.ReadWriter) (string, error) {
		offer, err := readCString(s, maxP9AnyMessage)
		if err != nil {
			return "", fmt.Errorf("read p9any offer: %w", err)
		}
		wantOffer := fmt.Sprintf("dp9ik@%s", authDomain)
		if offer != wantOffer {
			return "", fmt.Errorf("unexpected p9any offer %q", offer)
		}
		if err := writeCString(s, fmt.Sprintf("dp9ik %s", authDomain)); err != nil {
			return "", fmt.Errorf("write p9any selection: %w", err)
		}

		cchal := make([]byte, dp9ik.CHALLEN)
		if _, err := rand.Read(cchal); err != nil {
			return "", fmt.Errorf("generate client challenge: %w", err)
		}
		if _, err := s.Write(cchal); err != nil {
			return "", fmt.Errorf("write client challenge: %w", err)
		}

		serverOffer, err := readFixed(s, dp9ik.TICKREQLEN+dp9ik.PAKYLEN)
		if err != nil {
			return "", fmt.Errorf("read server offer: %w", err)
		}

		tr, _, err := dp9ik.UnmarshalTicketreq(serverOffer[:dp9ik.TICKREQLEN])
		if err != nil {
			return "", fmt.Errorf("decode ticketreq: %w", err)
		}
		if tr.Type != dp9ik.AuthPAK {
			return "", fmt.Errorf("unexpected ticketreq type %d", tr.Type)
		}
		fileServerY := append([]byte(nil), serverOffer[dp9ik.TICKREQLEN:]...)

		conn, err := net.DialTimeout("tcp", authServer, 5*time.Second)
		if err != nil {
			return "", fmt.Errorf("connect auth server: %w", err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		clientKey, err := dp9ik.PassToKey(password)
		if err != nil {
			return "", fmt.Errorf("derive client key: %w", err)
		}
		clientKey.AuthPAKHash(user)

		asReq := *tr
		copy(asReq.Hostid[:], []byte(user))
		copy(asReq.Uid[:], []byte(user))

		reqBuf, err := asReq.Marshal()
		if err != nil {
			return "", fmt.Errorf("marshal authpak request: %w", err)
		}
		if _, err := conn.Write(reqBuf); err != nil {
			return "", fmt.Errorf("write authpak request: %w", err)
		}
		if _, err := conn.Write(fileServerY); err != nil {
			return "", fmt.Errorf("write file-server pak y: %w", err)
		}

		clientPak := &dp9ik.PAKpriv{}
		clientY := clientPak.AuthPAKNew(clientKey, true)
		if _, err := conn.Write(clientY); err != nil {
			return "", fmt.Errorf("write client pak y: %w", err)
		}

		respCode, err := readFixed(conn, 1)
		if err != nil {
			return "", fmt.Errorf("read authpak status: %w", err)
		}
		if respCode[0] != dp9ik.AuthOK {
			return "", fmt.Errorf("unexpected authpak status %d", respCode[0])
		}

		pakResp, err := readFixed(conn, 2*dp9ik.PAKYLEN)
		if err != nil {
			return "", fmt.Errorf("read authpak response: %w", err)
		}
		serverY := append([]byte(nil), pakResp[:dp9ik.PAKYLEN]...)
		if err := clientPak.AuthPAKFinish(clientKey, pakResp[dp9ik.PAKYLEN:]); err != nil {
			return "", fmt.Errorf("finish client authpak: %w", err)
		}

		asReq.Type = dp9ik.AuthTreq
		reqBuf, err = asReq.Marshal()
		if err != nil {
			return "", fmt.Errorf("marshal ticket request: %w", err)
		}
		if _, err := conn.Write(reqBuf); err != nil {
			return "", fmt.Errorf("write ticket request: %w", err)
		}

		respCode, err = readFixed(conn, 1)
		if err != nil {
			return "", fmt.Errorf("read ticket status: %w", err)
		}
		if respCode[0] != dp9ik.AuthOK {
			return "", fmt.Errorf("unexpected ticket status %d", respCode[0])
		}

		clientTicket, serverTicketRaw, err := readAuthServerTickets(conn, clientKey)
		if err != nil {
			return "", fmt.Errorf("read tickets: %w", err)
		}

		auth := &dp9ik.Authenticator{Num: dp9ik.AuthAc}
		copy(auth.Chal[:], tr.Chal[:])
		if _, err := rand.Read(auth.Rand[:]); err != nil {
			return "", fmt.Errorf("generate client nonce: %w", err)
		}
		authBuf, err := auth.Marshal(clientTicket)
		if err != nil {
			return "", fmt.Errorf("marshal client authenticator: %w", err)
		}

		if _, err := s.Write(serverY); err != nil {
			return "", fmt.Errorf("write server pak y: %w", err)
		}

		serverMsg := make([]byte, 0, len(serverTicketRaw)+len(authBuf))
		serverMsg = append(serverMsg, serverTicketRaw...)
		serverMsg = append(serverMsg, authBuf...)
		if _, err := s.Write(serverMsg); err != nil {
			return "", fmt.Errorf("write server ticket: %w", err)
		}

		reply, err := readServerAuthenticator(s, clientTicket)
		if err != nil {
			return "", fmt.Errorf("read server authenticator: %w", err)
		}
		if reply.Num != dp9ik.AuthAs {
			return "", fmt.Errorf("unexpected server authenticator type %d", reply.Num)
		}
		if string(reply.Chal[:]) != string(cchal) {
			return "", fmt.Errorf("server authenticator challenge mismatch")
		}

		return user, nil
	}
}

func readAuthServerTickets(conn net.Conn, clientKey *dp9ik.Authkey) (*dp9ik.Ticket, []byte, error) {
	limit := 2 * dp9ik.MAXTICKETLEN
	buf := make([]byte, 0, limit)
	chunk := make([]byte, 256)
	var (
		clientTicket *dp9ik.Ticket
		ticketLen    int
	)

	for len(buf) <= limit {
		if clientTicket == nil && len(buf) > 0 {
			ticket, n, err := dp9ik.UnmarshalTicketWithLength(clientKey, buf)
			if err == nil {
				clientTicket = ticket
				ticketLen = n
				_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
			}
		}

		n, err := conn.Read(chunk)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && clientTicket != nil {
				return clientTicket, append([]byte(nil), buf[ticketLen:]...), nil
			}
			if err == io.EOF && clientTicket != nil {
				return clientTicket, append([]byte(nil), buf[ticketLen:]...), nil
			}
			return nil, nil, err
		}
		if n == 0 {
			if clientTicket != nil {
				return clientTicket, append([]byte(nil), buf[ticketLen:]...), nil
			}
			continue
		}
		buf = append(buf, chunk[:n]...)
	}

	return nil, nil, fmt.Errorf("ticket response exceeded %d bytes", limit)
}

func readServerAuthenticator(r io.Reader, ticket *dp9ik.Ticket) (*dp9ik.Authenticator, error) {
	limit := dp9ik.MAXAUTHENTLEN
	buf := make([]byte, 0, limit)
	chunk := make([]byte, 128)

	for len(buf) <= limit {
		if len(buf) > 0 {
			auth, _, err := dp9ik.UnmarshalAuthenticatorWithLength(ticket, buf)
			if err == nil {
				return auth, nil
			}
		}

		n, err := r.Read(chunk)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		buf = append(buf, chunk[:n]...)
	}

	return nil, fmt.Errorf("server authenticator exceeded %d bytes", limit)
}

func readFixed(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func readCString(r io.Reader, limit int) (string, error) {
	var out bytes.Buffer
	var b [1]byte

	for out.Len() < limit {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return "", err
		}
		if b[0] == 0 {
			return out.String(), nil
		}
		out.WriteByte(b[0])
	}

	return "", fmt.Errorf("p9any message exceeded %d bytes", limit)
}

func writeCString(w io.Writer, value string) error {
	buf := make([]byte, len(value)+1)
	copy(buf, value)
	_, err := w.Write(buf)
	return err
}

func Test9FrontAuthRequiresAuthentication(t *testing.T) {
	requireTestAuthServer(t)

	ns := NewNs(With9FrontAuth(testAuthDomain, testAuthUser, testAuthPassword))

	_, err := openTestClientWithOptions(t, ns, testAuthUser)
	if err == nil {
		t.Fatalf("expected unauthenticated attach to fail")
	}
	if !strings.Contains(err.Error(), "Not Authenticated") {
		t.Fatalf("unexpected unauthenticated attach error: %v", err)
	}
}

func Test9FrontAuthAllowsAuthenticatedAccess(t *testing.T) {
	requireTestAuthServer(t)

	ns := NewNs(With9FrontAuth(testAuthDomain, testAuthUser, testAuthPassword))
	ns.config.serverInfo.SetPort(4242)

	c, err := openTestClientWithOptions(
		t,
		ns,
		testAuthUser,
		client.WithAuth(newDP9IKTestClientAuth(testAuthServerAddr, testAuthDomain, testAuthUser, testAuthPassword)),
	)
	if err != nil {
		t.Fatalf("failed to authenticate client: %v", err)
	}

	if got := readRemoteFile(t, c, "/status"); got != "Ready" {
		t.Fatalf("unexpected status after auth attach: %q", got)
	}

	writeRemoteFile(t, c, "/ctl", "port")
	if got := readRemoteFile(t, c, "/data"); got != "4242" {
		t.Fatalf("unexpected port data after auth attach: %q", got)
	}
}

func Test9FrontAuthRejectsWrongPassword(t *testing.T) {
	requireTestAuthServer(t)

	ns := NewNs(With9FrontAuth(testAuthDomain, testAuthUser, testAuthPassword))

	_, err := openTestClientWithOptions(
		t,
		ns,
		testAuthUser,
		client.WithAuth(newDP9IKTestClientAuth(testAuthServerAddr, testAuthDomain, testAuthUser, "wrong-password")),
	)
	if err == nil {
		t.Fatalf("expected bad password attach to fail")
	}
	if strings.Contains(err.Error(), "failed to authenticate client") {
		t.Fatalf("unexpected bad password error: %v", err)
	}
}
