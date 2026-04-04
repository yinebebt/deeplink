// Package deeplink provides short link generation, click tracking, and
// preview pages with pluggable link processors and storage backends.
//
// Use [New] to create a [Service], register [Processor] implementations,
// and call [Service.Handler] to get an [http.Handler].
//
// See cmd/deeplink for a standalone server and example/ for usage examples.
package deeplink
