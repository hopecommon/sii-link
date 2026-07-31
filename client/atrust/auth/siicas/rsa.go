package siicas

import (
	"fmt"
	"math/big"
	"strings"
	"unicode/utf16"
)

// encryptPassword reproduces the base-65536 RSA representation used by the
// SII CAS login JavaScript. It is deliberately not a general RSA primitive.
func encryptPassword(password, exponentHex, modulusHex string) (string, error) {
	exponent := new(big.Int)
	if _, ok := exponent.SetString(exponentHex, 16); !ok || exponent.Sign() <= 0 {
		return "", fmt.Errorf("parse RSA exponent")
	}
	modulus := new(big.Int)
	if _, ok := modulus.SetString(modulusHex, 16); !ok || modulus.Sign() <= 0 {
		return "", fmt.Errorf("parse RSA modulus")
	}

	highIndex := (modulus.BitLen() - 1) / 16
	chunkSize := highIndex * 2
	if chunkSize <= 0 {
		return "", fmt.Errorf("RSA modulus is too small")
	}

	units := utf16.Encode([]rune(password))
	for len(units)%chunkSize != 0 {
		units = append(units, 0)
	}
	if len(units) == 0 {
		units = make([]uint16, chunkSize)
	}

	blocks := make([]string, 0, len(units)/chunkSize)
	for offset := 0; offset < len(units); offset += chunkSize {
		block := new(big.Int)
		for j := 0; j < chunkSize; j += 2 {
			digit := uint32(units[offset+j])
			if j+1 < chunkSize {
				digit += uint32(units[offset+j+1]) << 8
			}
			if digit == 0 {
				continue
			}
			part := new(big.Int).SetUint64(uint64(digit))
			part.Lsh(part, uint(16*(j/2)))
			block.Or(block, part)
		}

		ciphertext := new(big.Int).Exp(block, exponent, modulus)
		blocks = append(blocks, formatBase65536(ciphertext))
	}
	return strings.Join(blocks, " "), nil
}

func formatBase65536(value *big.Int) string {
	if value.Sign() == 0 {
		return "0000"
	}

	const digitBits = 16
	mask := big.NewInt(0xffff)
	remaining := new(big.Int).Set(value)
	digits := make([]string, 0, (value.BitLen()+digitBits-1)/digitBits)
	for remaining.Sign() > 0 {
		digit := new(big.Int).And(remaining, mask).Uint64()
		digits = append(digits, fmt.Sprintf("%04x", digit))
		remaining.Rsh(remaining, digitBits)
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return strings.Join(digits, "")
}
