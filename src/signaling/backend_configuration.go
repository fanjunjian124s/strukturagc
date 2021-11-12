/**
 * Standalone signaling server for the Nextcloud Spreed app.
 * Copyright (C) 2020 struktur AG
 *
 * @author Joachim Bauch <bauch@struktur.de>
 *
 * @license GNU AGPL version 3 or any later version
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */
package signaling

import (
	"log"
	"net/url"
	"reflect"
	"strings"

	"github.com/dlintw/goconf"
)

type Backend struct {
	id     string
	url    string
	secret []byte
	compat bool
}

func (b *Backend) Id() string {
	return b.id
}

func (b *Backend) Secret() []byte {
	return b.secret
}

func (b *Backend) IsCompat() bool {
	return b.compat
}

type BackendConfiguration struct {
	backends map[string][]*Backend

	// Deprecated
	allowAll      bool
	commonSecret  []byte
	compatBackend *Backend
}

func NewBackendConfiguration(config *goconf.ConfigFile) (*BackendConfiguration, error) {
	allowAll, _ := config.GetBool("backend", "allowall")
	commonSecret, _ := config.GetString("backend", "secret")
	backends := make(map[string][]*Backend)
	var compatBackend *Backend
	if allowAll {
		log.Println("WARNING: All backend hostnames are allowed, only use for development!")
		compatBackend = &Backend{
			id:     "compat",
			secret: []byte(commonSecret),
			compat: true,
		}
	} else if backendIds, _ := config.GetString("backend", "backends"); backendIds != "" {
		for host, configuredBackends := range getConfiguredHosts(config) {
			backends[host] = append(backends[host], configuredBackends...)
			for _, be := range configuredBackends {
				log.Printf("Backend %s added for %s", be.id, be.url)
			}
		}
	} else if allowedUrls, _ := config.GetString("backend", "allowed"); allowedUrls != "" {
		// Old-style configuration, only hosts are configured and are using a common secret.
		allowMap := make(map[string]bool)
		for _, u := range strings.Split(allowedUrls, ",") {
			u = strings.TrimSpace(u)
			if idx := strings.IndexByte(u, '/'); idx != -1 {
				log.Printf("WARNING: Removing path from allowed hostname \"%s\", check your configuration!", u)
				u = u[:idx]
			}
			if u != "" {
				allowMap[strings.ToLower(u)] = true
			}
		}

		if len(allowMap) == 0 {
			log.Println("WARNING: No backend hostnames are allowed, check your configuration!")
		} else {
			compatBackend = &Backend{
				id:     "compat",
				secret: []byte(commonSecret),
				compat: true,
			}
			hosts := make([]string, 0, len(allowMap))
			for host := range allowMap {
				hosts = append(hosts, host)
				backends[host] = []*Backend{compatBackend}
			}
			if len(hosts) > 1 {
				log.Println("WARNING: Using deprecated backend configuration. Please migrate the \"allowed\" setting to the new \"backends\" configuration.")
			}
			log.Printf("Allowed backend hostnames: %s\n", hosts)
		}
	}

	return &BackendConfiguration{
		backends: backends,

		allowAll:      allowAll,
		commonSecret:  []byte(commonSecret),
		compatBackend: compatBackend,
	}, nil
}

func (b *BackendConfiguration) RemoveBackend(host string) {
	delete(b.backends, host)
}

func (b *BackendConfiguration) UpsertHost(host string, backends []*Backend) {
	existingIndex := 0
	for _, existingBackend := range b.backends[host] {
		found := false
		index := 0
		for _, newBackend := range backends {
			if reflect.DeepEqual(existingBackend, newBackend) { // otherwise we could manually compare the struct members here
				found = true
				backends = append(backends[:index], backends[index+1:]...)
				break
			}
			index++
		}
		if !found {
			b.backends[host] = append(b.backends[host][:existingIndex], b.backends[host][existingIndex+1:]...)
		}
		existingIndex++
	}

	b.backends[host] = append(b.backends[host], backends...)
}

func getConfiguredBackendIDs(config *goconf.ConfigFile) (ids map[string]bool) {
	ids = make(map[string]bool)

	if backendIds, _ := config.GetString("backend", "backends"); backendIds != "" {
		for _, id := range strings.Split(backendIds, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}

			ids[id] = true
		}
	}

	return ids
}

func getConfiguredHosts(config *goconf.ConfigFile) (hosts map[string][]*Backend) {
	hosts = make(map[string][]*Backend)
	for id := range getConfiguredBackendIDs(config) {
		u, _ := config.GetString(id, "url")
		if u == "" {
			log.Printf("Backend %s is missing or incomplete, skipping", id)
			continue
		}

		if u[len(u)-1] != '/' {
			u += "/"
		}
		parsed, err := url.Parse(u)
		if err != nil {
			log.Printf("Backend %s has an invalid url %s configured (%s), skipping", id, u, err)
			continue
		}

		secret, _ := config.GetString(id, "secret")
		if u == "" || secret == "" {
			log.Printf("Backend %s is missing or incomplete, skipping", id)
			continue
		}

		hosts[parsed.Host] = append(hosts[parsed.Host], &Backend{
			id:     id,
			url:    u,
			secret: []byte(secret),
		})
	}

	return hosts
}

func (b *BackendConfiguration) Reload(config *goconf.ConfigFile) {
	if backendIds, _ := config.GetString("backend", "backends"); backendIds != "" {
		configuredHosts := getConfiguredHosts(config)

		// remove backends that are no longer configured
		for hostname := range b.backends {
			if _, ok := configuredHosts[hostname]; !ok {
				b.RemoveBackend(hostname)
			}
		}

		// rewrite backends adding newly configured ones and rewriting existing ones
		for hostname, configuredBackends := range configuredHosts {
			b.UpsertHost(hostname, configuredBackends)
		}
	}
}

func (b *BackendConfiguration) GetCompatBackend() *Backend {
	return b.compatBackend
}

func (b *BackendConfiguration) GetBackend(u *url.URL) *Backend {
	entries, found := b.backends[u.Host]
	if !found {
		if b.allowAll {
			return b.compatBackend
		}
		return nil
	}

	s := u.String()
	if s[len(s)-1] != '/' {
		s += "/"
	}
	for _, entry := range entries {
		if entry.url == "" {
			// Old-style configuration, only hosts are configured.
			return entry
		} else if strings.HasPrefix(s, entry.url) {
			return entry
		}
	}

	return nil
}

func (b *BackendConfiguration) GetBackends() []*Backend {
	var result []*Backend
	for _, entries := range b.backends {
		result = append(result, entries...)
	}
	return result
}

func (b *BackendConfiguration) IsUrlAllowed(u *url.URL) bool {
	if u == nil {
		// Reject all invalid URLs.
		return false
	}

	backend := b.GetBackend(u)
	return backend != nil
}

func (b *BackendConfiguration) GetSecret(u *url.URL) []byte {
	if u == nil {
		// Reject all invalid URLs.
		return nil
	}

	entry := b.GetBackend(u)
	if entry == nil {
		return nil
	}

	return entry.secret
}
