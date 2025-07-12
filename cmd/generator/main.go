package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Akimio521/freetype-go-generator/libfreetype"
	ccv4 "modernc.org/cc/v4"
	ccgo "modernc.org/ccgo/v4/lib"
)

const (
	freetypeHeaderDir = "freetype/include"
	aname             = "objs/.libs/libfreetype.a"
	result            = "ccgo.go"
	packageName       = "libfreetype"
)

var hostMap = map[string]string{
	"linux/386":     "i386-linux-gnu",
	"linux/amd64":   "x86_64-linux-gnu",
	"linux/arm":     "arm-linux-gnueabihf",
	"linux/arm64":   "aarch64-linux-gnu",
	"windows/amd64": "x86_64-w64-mingw32",
	"windows/arm64": "aarch64-w64-mingw32",
	"darwin/amd64":  "x86_64-apple-darwin",
	"darwin/arm64":  "aarch64-apple-darwin",
}

var (
	make       string
	sed        string
	targetOS   string
	targetArch string
	target     string
	host       string
	cc         string
	ar         string
	ranlib     string
	strip      string
	j          string = fmt.Sprint(runtime.GOMAXPROCS(-1))
)

func init() {
	targetOS = getEnv("TARGET_OS", runtime.GOOS)
	targetArch = getEnv("TARGET_ARCH", runtime.GOARCH)
	target = targetOS + "/" + targetArch
	var ok bool
	host, ok = hostMap[target]
	if !ok {
		panic(fmt.Sprintf("unsupported target %s", target))
	}

	cc = getEnv("CCGO_CC", "gcc")
	ar = getEnv("CCGO_AR", "ar")
	ranlib = getEnv("CCGO_RANLIB", "ranlib")
	strip = getEnv("CCGO_STRIP", "strip")
}

func getEnv(key, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	return val
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runInDir(dir string, fn func() error) error {
	oldDir, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(dir); err != nil {
		return err
	}

	return fn()
}

func CopyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		dstFile, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()
		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func cleanupSubmodule(submodulePath string) error {
	if err := runCommand("git", "-C", submodulePath, "clean", "-fdx"); err != nil {
		return err
	}
	if err := runCommand("git", "-C", submodulePath, "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	return nil
}

func checkoutSubmoduleTag(submodulePath, tag string) error {
	// if err := runCommand("git", "-C", submodulePath, "fetch", "--tags"); err != nil {
	// 	return fmt.Errorf("failed to fetch tags: %w", err)
	// }
	// if err := runCommand("git", "-C", submodulePath, "checkout", tag); err != nil {
	// 	return fmt.Errorf("failed to checkout tag %s: %w", tag, err)
	// }
	return nil
}

func main() {
	if ccgo.IsExecEnv() {
		if err := ccgo.NewTask(targetOS, targetArch, os.Args, os.Stdout, os.Stderr, nil).Main(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}

	fmt.Println("building for", target)
	fmt.Println("using host:", host)
	fmt.Println("using compiler:", cc)
	fmt.Println("using archiver:", ar)
	fmt.Println("using ranlib:", ranlib)
	fmt.Println("using strip:", strip)

	switch runtime.GOOS {
	case "linux":
		make = "make"
		sed = "sed"

	case "darwin":
		make = "make"
		sed = "gsed"

	case "freebsd", "openbsd":
		make = "gmake"
		sed = "gsed"

	case "windows":
		make = "make"
		sed = "sed"

	default:
		panic(fmt.Sprintf("unsupported target %s", target))
	}

	libRoot, err := filepath.Abs("freetype")
	if err != nil {
		panic(fmt.Sprintf("cannot get absolute path for freetype: %v", err))
	}

	// clean change for freetype submodule
	err = cleanupSubmodule(libRoot)
	if err != nil {
		panic(fmt.Sprintf("failed to cleanup submodule: %v", err))
	}

	err = CopyDir("internal", libRoot)
	if err != nil {
		panic(fmt.Sprintf("failed to copy overlay files: %v", err))
	}

	err = checkoutSubmoduleTag(libRoot, fmt.Sprintf("VER-%d-%d-%d", libfreetype.MAJOR, libfreetype.MINOR, libfreetype.PATCH))
	if err != nil {
		panic(fmt.Sprintf("failed to checkout submodule tag: %v", err))
	}

	buildFn := func() error {
		if err := runCommand("go", "mod", "init", "example.com/libfreetype"); err != nil {
			return err
		}
		if err := runCommand("go", "get", "modernc.org/libc@latest", "modernc.org/libz@latest"); err != nil {
			return err
		}

		// Run autogen.sh to generate configure script
		if err := runCommand("sh", "./autogen.sh"); err != nil {
			return err
		}

		ccgoConfig := []string{os.Args[0]}
		cflags := []string{"-DFT_CONFIG_OPTION_NO_ASSEMBLER"}
		switch target {
		case target:
			if err := runCommand(sed, "-i", `s/FT_SSE2 1/FT_SSE2 0/g`, "src/smooth/ftgrays.c"); err != nil {
				return err
			}
		}
		m64Double := ccv4.LongDouble64Flag(targetOS, targetArch)
		if m64Double != "" {
			ccgoConfig = append(ccgoConfig, m64Double)
		}

		freetypeConfigs := []string{"--disable-shared", "--with-brotli=no", "--with-bzip2=no", "--with-harfbuzz=no", "--with-png=no", "--with-zlib=yes"}
		cmdLine := "CFLAGS=" + strings.Join(cflags, " ") + " CC=" + cc + " AR=" + ar + " RANLIB=" + ranlib + " STRIP=" + strip + " ./configure " + strings.Join(freetypeConfigs, " ") + " --host=" + host
		err := runCommand("sh", "-c", cmdLine)
		if err != nil {
			return fmt.Errorf("failed to run configure: %w", err)
		}

		ccgoConfig = append(ccgoConfig,
			"--package-name", packageName,
			"--prefix-enumerator=_",
			"--prefix-external=x_",
			"--prefix-field=F",
			"--prefix-macro=m_",
			"--prefix-static-internal=_",
			"--prefix-static-none=_",
			"--prefix-tagged-enum=_",
			"--prefix-tagged-struct=T",
			"--prefix-tagged-union=T",
			"--prefix-typename=T",
			"--prefix-undefined=_",
			"-ignore-unsupported-alignment",
			"-I", freetypeHeaderDir,
		)

		err = ccgo.NewTask(targetOS, targetArch, append(ccgoConfig, "-exec", make, "-j", j, "library"), os.Stdout, os.Stderr, nil).Exec()
		if err != nil {
			return fmt.Errorf("failed to build freetype library: %w", err)
		}
		fmt.Println("freetype library built successfully, starting to generate ccgo bindings...")

		// Use absolute path for the library file
		libPath, err := filepath.Abs(aname)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for library: %w", err)
		}

		err = ccgo.NewTask(targetOS, targetArch, append(ccgoConfig, "-o", result, libPath), os.Stdout, os.Stderr, nil).Main()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ccgo bindings generation failed: %v\n", err)
			return fmt.Errorf("failed to generate ccgo bindings: %w", err)
		}
		fmt.Println("ccgo bindings generated successfully, starting to clean up...")

		err = runCommand(sed, "-i", `s/\<T__\([a-zA-Z0-9][a-zA-Z0-9_]\+\)/t__\1/g`, result)
		if err != nil {
			return fmt.Errorf("failed to replace T__ with t__: %w", err)
		}
		err = runCommand(sed, "-i", `s/\<x_\([a-zA-Z0-9][a-zA-Z0-9_]\+\)/X\1/g`, result)
		if err != nil {
			return fmt.Errorf("failed to replace x_ with X: %w", err)
		}
		err = runCommand(sed, "-i", `/^[[:space:]]*\/\/ Code generate/,+1d`, result)
		if err != nil {
			return fmt.Errorf("failed to remove code generation comment: %w", err)
		}
		return nil
	}
	err = runInDir(libRoot, buildFn)
	if err != nil {
		panic(fmt.Sprintf("failed to build freetype library and generate ccgo bindings: %v", err))
	}

	err = CopyFile(filepath.Join(libRoot, result), packageName+"/"+"ccgo"+"_"+targetOS+"_"+targetArch+".go")
	if err != nil {
		panic(fmt.Sprintf("failed to copy generated file: %v", err))
	}

	err = cleanupSubmodule(libRoot)
	if err != nil {
		panic(fmt.Sprintf("failed to cleanup submodule: %v", err))
	}

	fmt.Printf("Successfully generated ccgo bindings for %s and cleaned up submodule\n", target)
}
