package scalesetclient

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

type stub struct {
	server *httptest.Server

	mu sync.Mutex

	sessionStatus     int
	messageStatus     int
	messageEnvelope   any
	messageQueueEmpty bool

	maxCapacityHeaders []string
	acquiredIDs        [][]int64
	getMessageCalls    int

	statistics map[string]int
}

func newStub() *stub {
	s := &stub{
		sessionStatus: http.StatusOK,
		messageStatus: http.StatusOK,
		statistics: map[string]int{
			"totalAvailableJobs":     0,
			"totalAssignedJobs":      0,
			"totalRegisteredRunners": 0,
			"totalRunningJobs":       0,
		},
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.route))
	return s
}

func (s *stub) close() { s.server.Close() }

func (s *stub) url() string { return s.server.URL }

func (s *stub) configURL() string { return s.server.URL + "/testorg" }

func (s *stub) setStatistics(stats map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range stats {
		s.statistics[k] = v
	}
}

func (s *stub) observed() (getMessageCalls int, capacityHeaders []string, acquired [][]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getMessageCalls, append([]string(nil), s.maxCapacityHeaders...), append([][]int64(nil), s.acquiredIDs...)
}

func (s *stub) statisticsBody() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for k, v := range s.statistics {
		out[k] = v
	}
	return out
}

func (s *stub) route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case strings.HasSuffix(path, "/access_tokens"):
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      "installation-token",
			"expires_at": time.Now().Add(time.Hour),
		})

	case strings.HasSuffix(path, "/actions/runners/registration-token"):
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      "registration-token",
			"expires_at": time.Now().Add(time.Hour),
		})

	case path == "/api/v3/actions/runner-registration":
		writeJSON(w, http.StatusOK, map[string]any{
			"url":   s.url(),
			"token": mustAdminJWT(),
		})

	case path == "/_apis/runtime/runnerscalesets" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"count": 1,
			"value": []map[string]any{s.scaleSetBody()},
		})

	case path == "/_apis/runtime/runnerscalesets" && r.Method == http.MethodPost:
		writeJSON(w, http.StatusOK, s.scaleSetBody())

	case strings.HasSuffix(path, "/acquirejobs"):
		var ids []int64
		_ = json.NewDecoder(r.Body).Decode(&ids)
		s.mu.Lock()
		s.acquiredIDs = append(s.acquiredIDs, ids)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"count": len(ids), "value": ids})

	case strings.HasSuffix(path, "/generatejitconfig"):
		writeJSON(w, http.StatusOK, map[string]any{
			"runner":           map[string]any{"id": 7, "name": "runner-7", "runnerScaleSetId": 1},
			"encodedJITConfig": "encoded-jit-config",
		})

	case strings.Contains(path, "/sessions") && r.Method == http.MethodPost:
		s.mu.Lock()
		status := s.sessionStatus
		s.mu.Unlock()

		if status != http.StatusOK {
			writeJSON(w, status, map[string]any{
				"typeName": "SessionConflictException",
				"message":  "runner scale set already has an active session",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sessionId":               uuid.New().String(),
			"ownerName":               "stub-owner",
			"messageQueueUrl":         s.url() + "/messages",
			"messageQueueAccessToken": mustAdminJWT(),
			"runnerScaleSet":          s.scaleSetBody(),
			"statistics":              s.statisticsBody(),
		})

	case strings.Contains(path, "/sessions") && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)

	case path == "/messages" && r.Method == http.MethodGet:
		s.mu.Lock()
		s.getMessageCalls++
		s.maxCapacityHeaders = append(s.maxCapacityHeaders, r.Header.Get("X-ScaleSetMaxCapacity"))
		status, envelope, empty := s.messageStatus, s.messageEnvelope, s.messageQueueEmpty
		s.mu.Unlock()

		if empty || status == http.StatusAccepted {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if status != http.StatusOK {
			writeJSON(w, status, map[string]any{"message": "stub failure"})
			return
		}
		writeJSON(w, http.StatusOK, envelope)

	case strings.HasPrefix(path, "/messages/") && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, fmt.Sprintf("stub: unhandled %s %s", r.Method, path), http.StatusNotFound)
	}
}

func (s *stub) scaleSetBody() map[string]any {
	return map[string]any{
		"id":            1,
		"name":          "orka-runners",
		"runnerGroupId": 1,
		"labels":        []map[string]any{{"name": "orka-runners", "type": "System"}},
		"RunnerSetting": map[string]any{"disableUpdate": true},
		"statistics":    s.statisticsBody(),
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func jobMessagesEnvelope(messageID int, statistics map[string]int, jobs []map[string]any) map[string]any {
	return typedEnvelope(messageID, "RunnerScaleSetJobMessages", statistics, jobs)
}

func typedEnvelope(messageID int, messageType string, statistics map[string]int, jobs []map[string]any) map[string]any {
	body := ""
	if len(jobs) > 0 {
		encoded, err := json.Marshal(jobs)
		if err != nil {
			panic(err)
		}
		body = string(encoded)
	}

	return map[string]any{
		"messageId":   messageID,
		"messageType": messageType,
		"body":        body,
		"statistics":  statistics,
	}
}

func jobAvailable(runnerRequestID int64) map[string]any {
	return map[string]any{
		"messageType":     "JobAvailable",
		"runnerRequestId": runnerRequestID,
		"jobId":           fmt.Sprintf("job-%d", runnerRequestID),
		"acquireJobUrl":   "http://stub/acquire",
	}
}

func mustAdminJWT() string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	})
	signed, err := token.SignedString([]byte("stub-secret"))
	if err != nil {
		panic(err)
	}
	return signed
}

func mustAppPrivateKey() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}
