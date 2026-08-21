package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newAdmissionRouter(t *testing.T, opts AdmissionOptions, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Admission("test", opts))
	r.POST("/x", handler)
	return r
}

func post(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	return w
}

func TestAdmissionServesUnderTheLimit(t *testing.T) {
	r := newAdmissionRouter(t, AdmissionOptions{MaxInFlight: 2, AcquireTimeout: time.Second},
		func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for i := 0; i < 5; i++ {
		if got := post(r).Code; got != http.StatusOK {
			t.Fatalf("sequential request %d: got %d, want 200", i, got)
		}
	}
}

func TestAdmissionShedsOverTheLimit(t *testing.T) {
	release := make(chan struct{})
	admitted := make(chan struct{}, 1)

	r := newAdmissionRouter(t, AdmissionOptions{MaxInFlight: 1, AcquireTimeout: 20 * time.Millisecond},
		func(c *gin.Context) {
			admitted <- struct{}{}
			<-release
			c.String(http.StatusOK, "ok")
		})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		post(r)
	}()

	// Wait until the only slot is genuinely taken, rather than racing on a sleep.
	select {
	case <-admitted:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached the handler")
	}

	if got := post(r).Code; got != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", got)
	}

	close(release)
	wg.Wait()

	// The slot is returned, so the service recovers on its own.
	if got := post(r).Code; got != http.StatusOK {
		t.Fatalf("after the slot was released: got %d, want 200", got)
	}
}

func TestAdmissionShedIsFastRatherThanQueued(t *testing.T) {
	// The point of shedding is that a refusal costs a fraction of what serving
	// costs. A refusal that waited as long as the work would have is no use.
	release := make(chan struct{})
	admitted := make(chan struct{}, 1)
	const acquireTimeout = 30 * time.Millisecond

	r := newAdmissionRouter(t, AdmissionOptions{MaxInFlight: 1, AcquireTimeout: acquireTimeout},
		func(c *gin.Context) {
			admitted <- struct{}{}
			<-release
			c.String(http.StatusOK, "ok")
		})

	go post(r)
	select {
	case <-admitted:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached the handler")
	}

	start := time.Now()
	w := post(r)
	elapsed := time.Since(start)
	close(release)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
	if elapsed > 10*acquireTimeout {
		t.Fatalf("shedding took %s, far longer than the %s acquire timeout", elapsed, acquireTimeout)
	}
}

func TestAdmissionDefaultsAreApplied(t *testing.T) {
	// A zero-value config must not mean "no slots" or "never wait", which would
	// refuse every request.
	r := newAdmissionRouter(t, AdmissionOptions{},
		func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	if got := post(r).Code; got != http.StatusOK {
		t.Fatalf("got %d, want 200", got)
	}
}
