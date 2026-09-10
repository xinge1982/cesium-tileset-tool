package utils

import "crypto/md5"

const Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func Md5Shorten(str string) string {
	md5hash := md5.Sum([]byte(str))
	var hash uint64 = 0
	for i := 0; i < 8; i++ {
		hash = hash<<8 | uint64(md5hash[i]&0x00000000000000FF)
	}
	return encoder(hash)
}

func encoder(number uint64) string {
	Base := len(Alphabet)
	s := make([]byte, 0)
	for number > 0 {
		s = append(s, Alphabet[int(number%uint64(Base))])
		number = number / uint64(Base)
	}

	return string(s)
}
