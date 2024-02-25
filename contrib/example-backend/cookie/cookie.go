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

package cookie

import (
	"fmt"
	"net/http"
	"time"
)

func MakeCookie(req *http.Request, name string, value string, expiration time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/", // TODO: make configurable
		Domain:   "",  // TODO: add domain support
		Expires:  time.Now().Add(expiration),
		HttpOnly: true, // TODO: make configurable
		// setting to false so it works over http://localhost
		Secure:   false,             // TODO: make configurable
		SameSite: ParseSameSite(""), // TODO: make configurable
	}
}

func ParseSameSite(v string) http.SameSite {
	switch v {
	case "lax":
		return http.SameSiteLaxMode
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "":
		return 0
	default:
		panic(fmt.Sprintf("Invalid value for SameSite: %s", v))
	}
}
