package workloadapi

import (
	"context"
	"crypto"
	"crypto/x509"
	"sync"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/internal/test"
	"github.com/spiffe/go-spiffe/v2/internal/test/fakeworkloadapi"
	"github.com/spiffe/go-spiffe/v2/proto/spiffe/workload"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchX509SVID(t *testing.T) {
	ca := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()
	resp := &fakeworkloadapi.X509SVIDResponse{
		Bundle: ca.Roots(),
		SVIDs:  makeX509SVIDs(ca, "spiffe://example.org/foo", "spiffe://example.org/bar"),
	}
	wl.SetX509SVIDResponse(resp)
	svid, err := c.FetchX509SVID(context.Background())

	require.Nil(t, err)
	assert.Equal(t, "spiffe://example.org/foo", svid.ID.String())
	assert.Len(t, svid.Certificates, 1)
	assert.NotEmpty(t, svid.PrivateKey)
}

func TestFetchX509SVIDs(t *testing.T) {
	ca := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()

	resp := &fakeworkloadapi.X509SVIDResponse{
		Bundle: ca.Roots(),
		SVIDs:  makeX509SVIDs(ca, "spiffe://example.org/foo", "spiffe://example.org/bar"),
	}
	wl.SetX509SVIDResponse(resp)

	svids, err := c.FetchX509SVIDs(context.Background())
	require.Nil(t, err)
	require.Len(t, svids, 2)
	assert.Equal(t, "spiffe://example.org/foo", svids[0].ID.String())
	assert.NotEmpty(t, svids[0].PrivateKey)
	assert.Len(t, svids[0].Certificates, 1)
	assert.Equal(t, "spiffe://example.org/bar", svids[1].ID.String())
	assert.NotEmpty(t, svids[1].PrivateKey)
	assert.Len(t, svids[1].Certificates, 1)
}

func TestFetchX509Bundles(t *testing.T) {
	ca := test.NewCA(t)
	federatedCA := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()
	defer c.Close()

	resp := &fakeworkloadapi.X509SVIDResponse{
		Bundle:           ca.Roots(),
		SVIDs:            makeX509SVIDs(ca, "spiffe://example.org/foo", "spiffe://example.org/bar"),
		FederatedBundles: map[string][]*x509.Certificate{"spiffe://federated.org": federatedCA.Roots()},
	}
	wl.SetX509SVIDResponse(resp)

	bundles, err := c.FetchX509Bundles(context.Background())
	require.Nil(t, err)
	require.Len(t, bundles.Bundles(), 2)
	//TODO: inspect bundles
}

func TestFetchX509Context(t *testing.T) {
	ca := test.NewCA(t)
	federatedCA := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()
	defer c.Close()

	resp := &fakeworkloadapi.X509SVIDResponse{
		Bundle:           ca.Roots(),
		SVIDs:            makeX509SVIDs(ca, "spiffe://example.org/foo", "spiffe://example.org/bar"),
		FederatedBundles: map[string][]*x509.Certificate{"spiffe://federated.org": federatedCA.Roots()},
	}
	wl.SetX509SVIDResponse(resp)

	x509Ctx, err := c.FetchX509Context(context.Background())
	require.Nil(t, err)
	require.Len(t, x509Ctx.SVIDs, 2)
	//TODO: inspect svids
	assert.Len(t, x509Ctx.Bundles.Bundles(), 2)
	//TODO: inspect bundles
}

func TestWatchX509Context(t *testing.T) {
	ca := test.NewCA(t)
	federatedCA := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	tw := newTestWatcher(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		_ = c.WatchX509Context(ctx, tw)
		wg.Done()
	}()

	// test PermissionDenied
	tw.WaitForUpdates(1)
	assert.Len(t, tw.Errors(), 1)
	assert.Len(t, tw.X509Contexts(), 0)

	// test first update
	resp := &fakeworkloadapi.X509SVIDResponse{
		Bundle:           ca.Roots(),
		SVIDs:            makeX509SVIDs(ca, "spiffe://example.org/foo", "spiffe://example.org/bar"),
		FederatedBundles: map[string][]*x509.Certificate{"spiffe://federated.org": federatedCA.Roots()},
	}
	wl.SetX509SVIDResponse(resp)

	tw.WaitForUpdates(1)

	assert.Len(t, tw.Errors(), 1)
	assert.Len(t, tw.X509Contexts(), 1)
	assert.Len(t, tw.X509Contexts()[0].SVIDs, 2)

	// test second update
	resp = &fakeworkloadapi.X509SVIDResponse{
		Bundle:           ca.Roots(),
		SVIDs:            makeX509SVIDs(ca, "spiffe://example.org/baz"),
		FederatedBundles: map[string][]*x509.Certificate{"spiffe://federated.org": federatedCA.Roots()},
	}
	wl.SetX509SVIDResponse(resp)
	tw.WaitForUpdates(1)

	assert.Len(t, tw.Errors(), 1)
	assert.Len(t, tw.X509Contexts(), 2)
	assert.Len(t, tw.X509Contexts()[0].SVIDs, 2)
	assert.Len(t, tw.X509Contexts()[1].SVIDs, 1)

	// test error
	wl.Stop()
	tw.WaitForUpdates(1)
	assert.Len(t, tw.Errors(), 2)

	cancel()
	wg.Wait()
}

func TestFetchJWTSVID(t *testing.T) {
	ca := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, _ := New(context.Background(), WithAddr(wl.Addr()))
	defer c.Close()

	spiffeID := spiffeid.RequireFromString("spiffe://example.org/mytoken")
	respJWT := makeJWTSVIDResponse(ca, spiffeID.String(), "spiffe://example.org/audience", "spiffe://example.org/extra_audience")
	wl.SetJWTSVIDResponse(respJWT)

	params := jwtsvid.Params{
		Subject:        spiffeID,
		Audience:       "spiffe://example.org/audience",
		ExtraAudiences: []string{"spiffe://example.org/extra_audience"},
	}

	jwtSvid, err := c.FetchJWTSVID(context.Background(), params)

	require.Nil(t, err)
	assert.Equal(t, "spiffe://example.org/mytoken", jwtSvid.ID.String())
	assert.Equal(t, []string{"spiffe://example.org/audience", "spiffe://example.org/extra_audience"}, jwtSvid.Audience)
	assert.NotNil(t, jwtSvid.Claims)
	assert.NotEmpty(t, jwtSvid.Expiry)
	assert.NotEmpty(t, jwtSvid.Marshal())
}

func TestFetchJWTBundles(t *testing.T) {
	ca := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()

	jwk1 := ca.PublicJWTKey()
	jwk2 := ca.PublicJWTKey()
	keys := map[string]crypto.PublicKey{
		"1": jwk1,
		"2": jwk2,
	}
	wl.SetJWTBundle("spiffe://example.org", keys)

	bundleSet, err := c.FetchJWTBundles(context.Background())

	require.Nil(t, err)
	assert.Equal(t, 1, bundleSet.Len())
	td := spiffeid.RequireTrustDomainFromString("spiffe://example.org")
	assert.True(t, bundleSet.Has(td))
	bundle, ok := bundleSet.Get(td)
	require.True(t, ok)
	assert.Len(t, bundle.JWTAuthorities(), 2)
}

func TestWatchJWTBundles(t *testing.T) {
	ca := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())

	td := spiffeid.RequireTrustDomainFromString("spiffe://example.org")
	tw := newTestWatcher(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		_ = c.WatchJWTBundles(ctx, tw)
		wg.Done()
	}()

	// test PermissionDenied
	tw.WaitForUpdates(1)
	assert.Len(t, tw.Errors(), 1)
	assert.Len(t, tw.JwtBundles(), 0)

	// test first update
	jwk1 := ca.PublicJWTKey()
	keys := map[string]crypto.PublicKey{
		"1": jwk1,
	}
	wl.SetJWTBundle(td.String(), keys)

	tw.WaitForUpdates(1)

	assert.Len(t, tw.Errors(), 1)
	update := tw.JwtBundles()[len(tw.JwtBundles())-1]
	bundle, ok := update.Get(td)
	require.True(t, ok)
	assert.Len(t, bundle.JWTAuthorities(), 1)
	assert.NotNil(t, bundle.JWTAuthorities()["1"])

	// test second update
	jwk2 := ca.PublicJWTKey()
	keys = map[string]crypto.PublicKey{
		"1": jwk1,
		"2": jwk2,
	}
	wl.SetJWTBundle(td.String(), keys)

	tw.WaitForUpdates(1)

	assert.Len(t, tw.Errors(), 1)
	update = tw.JwtBundles()[len(tw.JwtBundles())-1]
	bundle, ok = update.Get(td)
	require.True(t, ok)
	assert.Len(t, bundle.JWTAuthorities(), 2)
	assert.NotNil(t, bundle.JWTAuthorities()["1"])
	assert.NotNil(t, bundle.JWTAuthorities()["2"])

	// test error
	wl.Stop()
	tw.WaitForUpdates(1)
	assert.Len(t, tw.Errors(), 2)

	cancel()
	wg.Wait()
}

func TestValidateJWTSVID(t *testing.T) {
	ca := test.NewCA(t)
	wl := fakeworkloadapi.NewWorkloadAPI(t)
	defer wl.Stop()
	c, err := New(context.Background(), WithAddr(wl.Addr()))
	require.NoError(t, err)
	defer c.Close()

	audience := []string{"spiffe://example.org/me", "spiffe://example.org/me_too"}
	token := ca.CreateJWTSVID("spiffe://example.org/workload", audience)

	t.Run("first audience is valid", func(t *testing.T) {
		jwtSvid, err := c.ValidateJWTSVID(context.Background(), token, audience[0])
		assert.Nil(t, err)
		assert.NotNil(t, jwtSvid)
	})

	t.Run("second audience is valid", func(t *testing.T) {
		jwtSvid, err := c.ValidateJWTSVID(context.Background(), token, audience[1])
		assert.Nil(t, err)
		assert.NotNil(t, jwtSvid)
	})

	t.Run("invalid audience returns error", func(t *testing.T) {
		jwtSvid, err := c.ValidateJWTSVID(context.Background(), token, "spiffe://example.org/not_me")
		assert.NotNil(t, err)
		assert.Nil(t, jwtSvid)
	})
}

func makeX509SVIDs(ca *test.CA, ids ...string) []fakeworkloadapi.X509SVID {
	svids := []fakeworkloadapi.X509SVID{}
	for _, id := range ids {
		svid, key := ca.CreateX509SVID(id)
		svids = append(svids, fakeworkloadapi.X509SVID{CertChain: svid, Key: key})
	}
	return svids
}

func makeJWTSVIDResponse(ca *test.CA, spiffeID string, audience ...string) *workload.JWTSVIDResponse {
	token := ca.CreateJWTSVID(spiffeID, audience)
	svids := []*workload.JWTSVID{
		{
			SpiffeId: spiffeID,
			Svid:     token,
		},
	}
	return &workload.JWTSVIDResponse{
		Svids: svids}
}

type testWatcher struct {
	t            *testing.T
	mu           sync.Mutex
	x509Contexts []*X509Context
	jwtBundles   []*jwtbundle.Set
	errors       []error
	updateSignal chan struct{}
}

func newTestWatcher(t *testing.T) *testWatcher {
	return &testWatcher{
		t:            t,
		updateSignal: make(chan struct{}, 100),
	}
}

func (w *testWatcher) X509Contexts() []*X509Context {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.x509Contexts
}

func (w *testWatcher) JwtBundles() []*jwtbundle.Set {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.jwtBundles
}

func (w *testWatcher) Errors() []error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.errors
}

func (w *testWatcher) OnX509ContextUpdate(u *X509Context) {
	w.mu.Lock()
	w.x509Contexts = append(w.x509Contexts, u)
	w.mu.Unlock()
	w.updateSignal <- struct{}{}
}

func (w *testWatcher) OnX509ContextWatchError(err error) {
	w.mu.Lock()
	w.errors = append(w.errors, err)
	w.mu.Unlock()
	w.updateSignal <- struct{}{}
}
func (w *testWatcher) OnJWTBundlesUpdate(u *jwtbundle.Set) {
	w.mu.Lock()
	w.jwtBundles = append(w.jwtBundles, u)
	w.mu.Unlock()
	w.updateSignal <- struct{}{}
}

func (w *testWatcher) OnJWTBundlesWatchError(err error) {
	w.mu.Lock()
	w.errors = append(w.errors, err)
	w.mu.Unlock()
	w.updateSignal <- struct{}{}
}

func (w *testWatcher) WaitForUpdates(expectedNumUpdates int) {
	numUpdates := 0
	timeoutSignal := time.After(10 * time.Second)
	for {
		select {
		case <-w.updateSignal:
			numUpdates++
		case <-timeoutSignal:
			require.Fail(w.t, "Timeout exceeding waiting for updates.")
		}
		if numUpdates == expectedNumUpdates {
			return
		}
	}
}
