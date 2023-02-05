package handlers

//
// security.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// tls, authorization configuration.
// based on github.com/prometheus/exporter-toolkit/web

import (
	"crypto/tls"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/exporter-toolkit/web"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v2"
	"prom-dbquery_exporter.app/metrics"
	"prom-dbquery_exporter.app/support"
)

// listenAndServe start webserver
func listenAndServe(server *http.Server, tlsConfigPath string) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}

	defer listener.Close()

	if tlsConfigPath == "" {
		support.Logger.Info().Msg("TLS is disabled.")
		return server.Serve(listener)
	}

	if err := web.Validate(tlsConfigPath); err != nil {
		return err
	}

	c, err := getConfig(tlsConfigPath)
	if err != nil {
		return err
	}

	var handler http.Handler = http.DefaultServeMux
	if server.Handler != nil {
		handler = server.Handler
	}
	server.Handler = newSecWebHandler(c, handler)

	config, err := web.ConfigToTLSConfig(&c.TLSConfig)
	if err == nil {
		if !c.HTTPConfig.HTTP2 {
			server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
		}
		// Valid TLS config.
		support.Logger.Info().Msg("TLS is enabled.")
	} else {
		support.Logger.Info().Msg("TLS is disabled.")
		return server.Serve(listener)
	}

	server.TLSConfig = config
	server.TLSConfig.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		config, err := getConfig(tlsConfigPath)
		if err != nil {
			return nil, err
		}
		tlsconf, err := web.ConfigToTLSConfig(&config.TLSConfig)
		if err != nil {
			return nil, err
		}
		tlsconf.NextProtos = server.TLSConfig.NextProtos
		return tlsconf, nil
	}
	return server.ServeTLS(listener, "", "")
}

func getConfig(configPath string) (*web.Config, error) {
	content, err := os.ReadFile(configPath) // #nosec
	if err != nil {
		return nil, err
	}
	c := &web.Config{
		TLSConfig: web.TLSConfig{
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
		},
		HTTPConfig: web.HTTPConfig{HTTP2: true},
	}
	err = yaml.UnmarshalStrict(content, c)
	c.TLSConfig.SetDirectory(filepath.Dir(configPath))
	return c, err
}

type secWebHandler struct {
	handler http.Handler
	cache   map[string]bool
	headers map[string]string
	users   map[string]config_util.Secret

	mtx sync.Mutex
}

func newSecWebHandler(conf *web.Config, handler http.Handler) *secWebHandler {
	if cu := len(conf.Users); cu > 0 {
		support.Logger.Info().Int("users", cu).Msg("Authorization enabled")
	} else {
		support.Logger.Info().Msg("Authorization disabled")
	}

	return &secWebHandler{
		handler: handler,
		cache:   make(map[string]bool),
		headers: conf.HTTPConfig.Header,
		users:   conf.Users,
	}
}

func (wh *secWebHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Configure http headers.
	for k, v := range wh.headers {
		w.Header().Set(k, v)
	}

	if len(wh.users) == 0 {
		wh.handler.ServeHTTP(w, r)
		return
	}

	user, pass, auth := r.BasicAuth()
	if auth {
		hashedPassword, validUser := wh.users[user]

		if !validUser {
			// The user is not found. Use a fixed password hash to
			// prevent user enumeration by timing requests.
			// This is a bcrypt-hashed version of "fakepassword".
			hashedPassword = "$2y$10$QOauhQNbBCuQDKes6eFzPeMqBSjb7Mr5DUmpZ/VcEd00UAV/LDeSi" // #nosec
		}

		cacheKey := hex.EncodeToString(append(append([]byte(user), []byte(hashedPassword)...),
			[]byte(pass)...))

		wh.mtx.Lock()
		authOk, ok := wh.cache[cacheKey]
		if !ok {
			// This user, hashedPassword, password is not cached.
			err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(pass))
			authOk = err == nil
			wh.cache[cacheKey] = authOk
		}

		wh.mtx.Unlock()

		if authOk && validUser {
			wh.handler.ServeHTTP(w, r)
			return
		}
	}

	metrics.IncProcessErrorsCnt("unauthorized")
	w.Header().Set("WWW-Authenticate", "Basic")
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}
