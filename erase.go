package npln

// Account erasure (openpak/prds#120). DELETE /internal/accounts/{account_id}/data with
// `Authorization: Bearer <ACCOUNT_ERASE_KEY>` is served on the title's plain health listener
// (container network only). Every title container holds its own live state and its own title's
// rows, so the website calls every nplnd container.
//
// The NPLN user id of a player is derived from the BAAS user id, which is the subject of the
// account's "switch" link in the core, so the core's links are asked for the subjects.

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	accountv1 "github.com/openpak/account-api/openpak/account/v1"
)

var userB32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// UserID is the stable NPLN user id ("u-" + 20 base32 chars) derived from a BAAS user id; every
// title derives it the same way.
func UserID(baas string) string {
	sum := sha256.Sum256([]byte("npln-user:" + baas))
	return "u-" + userB32.EncodeToString(sum[:12])
}

// Eraser removes what a title package holds for the NPLN user ids and reports the rows it removed.
type Eraser func(ctx context.Context, uids []string) (int, error)

var (
	eraseMu sync.Mutex
	erasers []Eraser
)

// RegisterEraser adds a title package's eraser; called from init.
func RegisterEraser(e Eraser) {
	eraseMu.Lock()
	defer eraseMu.Unlock()
	erasers = append(erasers, e)
}

// LinkLister answers which switch subjects (BAAS user ids) the core holds for an account.
type LinkLister interface {
	SwitchSubjects(ctx context.Context, accountID string) ([]string, error)
}

type coreLinks struct{ links accountv1.LinksClient }

func newCoreLinks(addr, key string) (*coreLinks, error) {
	if len(key) < 32 {
		return nil, errors.New("ACCOUNT_INTERNAL_KEY must be set (>=32 bytes)")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any,
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(metadata.AppendToOutgoingContext(ctx, "x-internal-key", key), method, req, reply, cc, opts...)
		}))
	if err != nil {
		return nil, err
	}
	return &coreLinks{links: accountv1.NewLinksClient(conn)}, nil
}

func (c *coreLinks) SwitchSubjects(ctx context.Context, accountID string) ([]string, error) {
	resp, err := c.links.ListAccountLinks(ctx, &accountv1.ListAccountLinksRequest{AccountId: accountID, Namespace: "switch"})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range resp.GetLinks() {
		if l.GetNamespace() == "switch" && l.GetSubjectId() != "" {
			out = append(out, l.GetSubjectId())
		}
	}
	return out, nil
}

// EraseAccount erases the account on this container: every registered eraser runs for its users.
func EraseAccount(ctx context.Context, links LinkLister, service, accountID string) (int, error) {
	if links == nil {
		return 0, errors.New("account core not configured")
	}
	subs, err := links.SwitchSubjects(ctx, accountID)
	if err != nil {
		return 0, err
	}
	uids := make([]string, 0, len(subs))
	for _, s := range subs {
		uids = append(uids, UserID(s))
	}
	rows := 0
	if len(uids) > 0 {
		eraseMu.Lock()
		list := append([]Eraser(nil), erasers...)
		eraseMu.Unlock()
		for _, e := range list {
			n, err := e(ctx, uids)
			rows += n
			if err != nil {
				return rows, err
			}
		}
	}
	log.Printf("account erased account=%s rows=%d service=%s", accountID, rows, service)
	return rows, nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// EraseHandler serves the erase endpoint. An empty key serves 503: never open.
func EraseHandler(key, service string, links LinkLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if key == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "erase not configured"})
			return
		}
		h := r.Header.Get("Authorization")
		got := strings.TrimPrefix(h, "Bearer ")
		if got == h || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		id := r.PathValue("account_id")
		if !uuidRe.MatchString(id) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "account_id must be a UUID"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		rows, err := EraseAccount(ctx, links, service, strings.ToLower(id))
		if err != nil {
			log.Print("erase failed")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erase failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"service": service, "rows": rows})
	}
}

// MountErase wires the core client from the environment and mounts the endpoint on mux.
func MountErase(mux *http.ServeMux, service string) {
	key := os.Getenv("ACCOUNT_ERASE_KEY")
	var links LinkLister
	if key != "" {
		addr := os.Getenv("ACCOUNT_GRPC_ADDR")
		if addr == "" {
			addr = "openpak-account:20001"
		}
		if l, err := newCoreLinks(addr, os.Getenv("ACCOUNT_INTERNAL_KEY")); err != nil {
			log.Printf("ERROR erase endpoint cannot reach the account core: %v", err)
		} else {
			links = l
		}
	}
	mux.HandleFunc("DELETE /internal/accounts/{account_id}/data", EraseHandler(key, service, links))
}
