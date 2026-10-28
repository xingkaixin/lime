package main

// fetchAvatar finds no picture in browsers: net/http and the TLS it
// links would make the demo's WebAssembly several megabytes larger.
func fetchAvatar(string) ([]byte, error) { return []byte{}, nil }
