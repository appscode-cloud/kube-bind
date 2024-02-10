/*
Copyright 2022 The Kube Bind Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package version

import (
	"fmt"
	"strings"
)

func BinaryVersion(s string) (string, error) {
	if strings.HasPrefix(s, "v0.0.0-") {
		return "v0.0.0", nil // special version if no ldflags are set
	}
	parts := strings.SplitN(s, "+", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("failed to parse version %q", s)
	}

	prefix := "kubeware"
	if !strings.HasPrefix(parts[1], prefix) {
		return "", fmt.Errorf("failed to parse version %q", s)
	}
	return strings.SplitN(strings.TrimPrefix(parts[1], prefix+"-"), "-", 2)[0], nil
}
