package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAES(t *testing.T) {

	var (
		testCases = []string{"123123123123", ""}
		key       = []byte("LKHlhb899Y09olUi")
	)

	// assert equality
	for _, v := range testCases {
		encrypted, encrypterr := encryptValue(v, key)
		decrypted, decrypterr := decryptValue(encrypted, key)

		assert.Equal(t, v, decrypted, "Decrypted Value and Initial Value should be equal")

		// assert inequality
		if v != "" {
			assert.NotEqual(t, encrypted, v, "Encrypted Value and Initial Value should not be equal")
		} else {
			assert.Equal(t, encrypted, v, "Encrypted Value and Initial Value for nill strings should be equal")
		}

		// assert for nil (good for errors)
		assert.Nil(t, encrypterr, "Encryption Error")
		assert.Nil(t, decrypterr, "Decryption Error")
	}

}
