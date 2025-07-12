package main

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"github.com/Akimio521/freetype-go-generator/libfreetype"
	_ "modernc.org/cc/v4"
	_ "modernc.org/ccgo/v4/lib"
	_ "modernc.org/fileutil/ccgo"
	"modernc.org/libc"
	_ "modernc.org/libz"
)

var (
	faceHandle uintptr
	libHandle  uintptr
)

func main() {
	tls := libc.NewTLS()

	defer tls.Close()

	rc := libfreetype.XFT_Init_FreeType(tls, uintptr(unsafe.Pointer(&libHandle)))
	if rc != 0 {
		panic(fmt.Sprintf("FT_Init_FreeType failed: rc=%v", rc))
	}

	pth, err := libc.CString(filepath.Join("testdata", "Go-Regular.ttf"))
	if err != nil {
		panic(fmt.Sprintf("CString: %v", err))
	}

	defer libc.Xfree(tls, pth)

	rc = libfreetype.XFT_New_Face(tls, libHandle, pth, 0, uintptr(unsafe.Pointer(&faceHandle)))
	if rc != 0 {
		panic(fmt.Sprintf("FT_New_Face failed: rc=%v", rc))
	}

	face := *(*libfreetype.TFT_FaceRec)(unsafe.Pointer(faceHandle))
	fmt.Println("Face loaded successfully")
	if g, e := libc.GoString(face.Ffamily_name), "Go"; g != e {
		panic(fmt.Sprintf("face.Ffamily_name=%q, expected %q", g, e))
	}

	if g, e := libc.GoString(face.Fstyle_name), "Regular"; g != e {
		panic(fmt.Sprintf("face.Fstyle_name=%q, expected %q", g, e))
	}
}
