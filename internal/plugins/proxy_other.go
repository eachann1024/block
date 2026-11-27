//go:build !darwin

package plugins

import "net/url"

func systemProxyForRequest(_ *url.URL) *url.URL {
	return nil
}
