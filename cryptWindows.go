//go:build windows

package main

import (
	"encoding/base64"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

type DPAPI struct {
	procEncryptData *windows.LazyProc
	procDecryptData *windows.LazyProc
}

type dpapiDataBlob struct {
	cbData uint32
	pbData *byte
}

func encryptConfigStrings() {
	d := NewDPAPI()
	config.FallbackSMTPuser = confStringEncrypt(config.FallbackSMTPuser, d)
	config.FallbackSMTPpass = confStringEncrypt(config.FallbackSMTPpass, d)
	config.OAuth2Config.ClientID = confStringEncrypt(config.OAuth2Config.ClientID, d)
	config.OAuth2Config.ClientSecret = confStringEncrypt(config.OAuth2Config.ClientSecret, d)
	config.OAuth2Config.TenantID = confStringEncrypt(config.OAuth2Config.TenantID, d)
}

func confStringEncrypt(c string, d *DPAPI) string {
	if len(c) > 19 && c[:19] == "__SYSTEMENCRYPTED__" {
		return c // Already encrypted
	}
	enc, err := d.Encrypt([]byte(c), []byte("smtps@wAppX1829"), true)
	if err != nil {
		return c
	}
	return "__SYSTEMENCRYPTED__" + enc
}

func decryptConfigStrings() {
	d := NewDPAPI()
	config.FallbackSMTPuser = confStringDecrypt(config.FallbackSMTPuser, d)
	config.FallbackSMTPpass = confStringDecrypt(config.FallbackSMTPpass, d)
	config.OAuth2Config.ClientID = confStringDecrypt(config.OAuth2Config.ClientID, d)
	config.OAuth2Config.ClientSecret = confStringDecrypt(config.OAuth2Config.ClientSecret, d)
	config.OAuth2Config.TenantID = confStringDecrypt(config.OAuth2Config.TenantID, d)
}

func confStringDecrypt(c string, d *DPAPI) string {
	if len(c) < 19 || c[:19] != "__SYSTEMENCRYPTED__" {
		return c
	}
	dec, err := d.Decrypt(c[19:], []byte("smtps@wAppX1829"))
	if err != nil {
		return c
	}
	return string(dec)
}

// NewDPAPI encrypt/decrypt data with the DPAPI (https://en.wikipedia.org/wiki/Data_Protection_API)
func NewDPAPI() *DPAPI {
	var dllcrypt32 = windows.NewLazySystemDLL("Crypt32.dll")
	return &DPAPI{
		procEncryptData: dllcrypt32.NewProc("CryptProtectData"),
		procDecryptData: dllcrypt32.NewProc("CryptUnprotectData"),
	}
}

/*
Encrypt encrypts the data
  - entropy is optional (nil is fine). It can be used to ensure that only the same entropy can decrypt the data
  - localMachine determines if the DPAPI is the user's or the local machine's
*/
func (d *DPAPI) Encrypt(data, entropy []byte, localMachine bool) (string, error) {
	var cryptProtectUIForbidden uint32 = 0x1
	var cryptProtectLocalMachine uint32 = 0x4
	if localMachine {
		return d.encryptBytes(data, entropy, cryptProtectUIForbidden|cryptProtectLocalMachine)
	} else {
		return d.encryptBytes(data, entropy, cryptProtectUIForbidden)
	}
}

/*
Decrypt decrypts the data
  - entropy is optional (nil is fine). It can be used to ensure that only the same entropy can decrypt the data
*/
func (d *DPAPI) Decrypt(data string, entropy []byte) ([]byte, error) {
	cipherbytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	var cryptProtectUIForbidden uint32 = 0x1
	return d.decryptBytes(cipherbytes, entropy, cryptProtectUIForbidden) // 0x1=cryptProtectUIForbidden
}

func (d *DPAPI) encryptBytes(data []byte, entropy []byte, cf uint32) (string, error) {
	var (
		outblob dpapiDataBlob
		r       uintptr
		err     error
	)

	if len(entropy) > 0 {
		r, _, err = d.procEncryptData.Call(
			uintptr(unsafe.Pointer(newBlob(data))),
			0,
			uintptr(unsafe.Pointer(newBlob(entropy))),
			0,
			0,
			uintptr(cf),
			uintptr(unsafe.Pointer(&outblob)))
	} else {
		r, _, err = d.procEncryptData.Call(
			uintptr(unsafe.Pointer(newBlob(data))),
			0,
			0,
			0,
			0,
			uintptr(cf),
			uintptr(unsafe.Pointer(&outblob)))
	}
	if r == 0 {
		return "", errors.Wrap(err, "procencryptdata")
	}

	enc := outblob.toByteArray()
	return base64.StdEncoding.EncodeToString(enc), outblob.free()
}

// DecryptBytes decrypts a byte array returning a byte array
func (d *DPAPI) decryptBytes(data, entropy []byte, cf uint32) ([]byte, error) {
	var (
		outblob dpapiDataBlob
		r       uintptr
		err     error
	)
	if len(entropy) > 0 {
		r, _, err = d.procDecryptData.Call(
			uintptr(unsafe.Pointer(newBlob(data))),
			0,
			uintptr(unsafe.Pointer(newBlob(entropy))),
			0,
			0,
			uintptr(cf),
			uintptr(unsafe.Pointer(&outblob)))
	} else {
		r, _, err = d.procDecryptData.Call(
			uintptr(unsafe.Pointer(newBlob(data))),
			0,
			0,
			0,
			0,
			uintptr(cf),
			uintptr(unsafe.Pointer(&outblob)))
	}
	if r == 0 {
		return nil, errors.Wrap(err, "procdecryptdata")
	}

	dec := outblob.toByteArray()
	outblob.zeroMemory()
	return dec, outblob.free()
}

func newBlob(d []byte) *dpapiDataBlob {
	if len(d) == 0 {
		return &dpapiDataBlob{}
	}
	return &dpapiDataBlob{
		pbData: &d[0],
		cbData: uint32(len(d)),
	}
}

func (b *dpapiDataBlob) toByteArray() []byte {
	d := make([]byte, b.cbData)
	copy(d, (*[1 << 30]byte)(unsafe.Pointer(b.pbData))[:])
	return d
}

func (b *dpapiDataBlob) zeroMemory() {
	zeros := make([]byte, b.cbData)
	copy((*[1 << 30]byte)(unsafe.Pointer(b.pbData))[:], zeros)
}

func (b *dpapiDataBlob) free() error {
	_, err := windows.LocalFree(windows.Handle(unsafe.Pointer(b.pbData)))
	if err != nil {
		return errors.Wrap(err, "localfree")
	}

	return nil
}
