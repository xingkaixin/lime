//go:build !js

package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// avatarClient fetches the pictures of commit authors.
var avatarClient = &http.Client{Timeout: 15 * time.Second}

func fetchAvatar(email string) ([]byte, error) {
	u := avatarURL(email)
	if u == "" {
		return []byte{}, nil
	}
	resp, err := avatarClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return []byte{}, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
