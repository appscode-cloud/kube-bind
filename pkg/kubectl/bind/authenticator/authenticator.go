/*
Copyright AppsCode Inc. and Contributors

Licensed under the AppsCode Community License 1.0.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/appscode/licenses/raw/1.0.0/AppsCode-Community-1.0.0.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package authenticator

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	kubebindv1alpha1 "go.bytebuilders.dev/kube-bind/apis/kubebind/v1alpha1"
	"go.bytebuilders.dev/kube-bind/pkg/template"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

var (
	kubebindSchema = runtime.NewScheme()
	kubebindCodecs = serializer.NewCodecFactory(kubebindSchema)
)

func init() {
	utilruntime.Must(kubebindv1alpha1.AddToScheme(kubebindSchema))
}

type LocalhostCallbackAuthenticator struct {
	port   int
	server *http.Server

	mu          sync.Mutex // synchronizes mutations of fields below
	done        chan struct{}
	response    runtime.Object
	responseGvk *schema.GroupVersionKind
	redirectURL string
}

func NewLocalhostCallbackAuthenticator(redirectURL string) *LocalhostCallbackAuthenticator {
	return &LocalhostCallbackAuthenticator{
		done:        make(chan struct{}),
		redirectURL: redirectURL,
	}
}

func (d *LocalhostCallbackAuthenticator) Start() error {
	if d.server != nil {
		return errors.New("already started")
	}

	address, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", d.callback)
	d.server = &http.Server{Handler: mux}

	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		return err
	}
	d.port = listener.Addr().(*net.TCPAddr).Port

	go func() {
		d.server.Serve(listener) // nolint: errcheck
	}()

	return nil
}

// Endpoint returns the URL this server is listening to.
// Start() must be called prior to this.
func (d *LocalhostCallbackAuthenticator) Endpoint() string {
	return fmt.Sprintf("http://%s/callback", net.JoinHostPort("localhost", strconv.Itoa(d.port)))
}

func (d *LocalhostCallbackAuthenticator) WaitForResponse(ctx context.Context) (runtime.Object, *schema.GroupVersionKind, error) {
	select {
	case <-d.done:
		return d.response, d.responseGvk, nil
	case <-ctx.Done():
		// 5 seconds shutdown timeout
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := d.server.Shutdown(shutdownCtx); err != nil {
			return nil, nil, fmt.Errorf("error while waiting for response: %w: error shutting down server: %v", shutdownCtx.Err(), err)
		}
		return nil, nil, fmt.Errorf("error while waiting for response: %w", shutdownCtx.Err())
	}
}

type SuccessOptions struct {
	RedirectURL string
}

func (d *LocalhostCallbackAuthenticator) callback(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	select {
	case <-d.done:
		w.Write([]byte("Already authenticated")) // nolint: errcheck
		return
	default:
	}

	authData := r.URL.Query().Get("response")
	logger := klog.FromContext(r.Context())
	logger.V(7).Info("Received auth data", "data", authData)

	decoded, err := base64.StdEncoding.DecodeString(authData)
	if err != nil {
		logger.Error(err, "error decoding authData")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	response, gvk, err := kubebindCodecs.UniversalDeserializer().Decode(decoded, nil, nil)
	if err != nil {
		logger.Error(err, "error decoding authResponse")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	d.response = response
	d.responseGvk = gvk
	close(d.done)

	op := SuccessOptions{RedirectURL: d.redirectURL}
	bs := bytes.Buffer{}
	if err = template.GetTemplate(template.TemplateSuccessPage).Execute(&bs, op); err != nil {
		logger.Error(err, "error executing success template")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Write(bs.Bytes()) // nolint: errcheck
}
