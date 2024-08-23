/*
 *
 * Copyright 2021 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package aeadcrypter

import (
	"crypto/cipher"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// Supported key size in bytes.
const (
	Chacha20Poly1305KeySize = 32
)

// chachapoly is the struct that holds a CHACHA-POLY cipher for the S2A AEAD
// crypter.
type chachapoly struct {
	aead cipher.AEAD
}

// NewChachaPoly creates a Chacha-Poly crypter instance. Note that the key must
// be Chacha20Poly1305KeySize bytes in length.
func NewChachaPoly(key []byte) (S2AAEADCrypter, error) {
	if len(key) != Chacha20Poly1305KeySize {
		return nil, fmt.Errorf("%d bytes, given: %d", Chacha20Poly1305KeySize, len(key))
	}
	c, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	return &chachapoly{aead: c}, nil
}

// Encrypt is the encryption function. dst can contain bytes at the beginning of
// the ciphertext that will not be encrypted but will be authenticated. If dst
// has enough capacity to hold these bytes, the ciphertext and the tag, no
// allocation and copy operations will be performed. dst and plaintext may
// fully overlap or not at all.
func (s *chachapoly) Encrypt(dst, plaintext, nonce, aad []byte) ([]byte, error) {
	return encrypt(s.aead, dst, plaintext, nonce, aad)
}

func (s *chachapoly) Decrypt(dst, ciphertext, nonce, aad []byte) ([]byte, error) {
	return decrypt(s.aead, dst, ciphertext, nonce, aad)
}

func (s *chachapoly) TagSize() int {
	return TagSize
}
