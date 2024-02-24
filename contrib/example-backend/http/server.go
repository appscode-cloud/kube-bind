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

package http

import (
	"context"
	"net"
	"net/http"
	"strconv"

	"go.bytebuilders.dev/kube-bind/contrib/example-backend/options"

	"github.com/gorilla/mux"
)

type Server struct {
	options  *options.Serve
	listener net.Listener
	Router   *mux.Router
}

func NewServer(options *options.Serve) (*Server, error) {
	server := &Server{
		options: options,
		Router:  mux.NewRouter(),
	}

	if options.Listener == nil {
		var err error
		addr := options.ListenAddress
		if options.ListenIP != "" {
			addr = net.JoinHostPort(options.ListenIP, strconv.Itoa(options.ListenPort))
		}
		server.listener, err = net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
	} else {
		server.listener = options.Listener
	}

	return server, nil
}

func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *Server) Start(ctx context.Context) error {
	server := &http.Server{
		Handler: s.Router,
	}
	go func() {
		<-ctx.Done()
		server.Close() // nolint:errcheck
	}()

	go func() {
		if s.options.KeyFile == "" {
			server.Serve(s.listener) // nolint:errcheck
		} else {
			server.ServeTLS(s.listener, s.options.CertFile, s.options.KeyFile) // nolint:errcheck
		}
	}()

	return nil
}
