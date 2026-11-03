// Command ai-upscaler 使用 AI（Real-ESRGAN）在本地对图片进行高清修复/放大。
//
// 它是 Real-ESRGAN-ncnn-vulkan 推理引擎的 Go 封装：首次运行时自动下载
// 官方预编译引擎与模型到用户目录，之后所有计算均在本地完成，无需联网、
// 不经过任何云服务。
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	appName = "ai-upscaler"
	// 官方 Real-ESRGAN v0.2.5.0 发行包（内含引擎 + 全部模型，约 45~50MB）
	pkgVersion = "20220424"
)

// 各平台官方预编译包
var pkgURLs = map[string]string{
	"darwin":  "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-macos.zip",
	"linux":   "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-ubuntu.zip",
	"windows": "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-windows.zip",
}

// 友好模型名 -> 引擎模型名
var modelAliases = map[string]string{
	"general":    "realesrgan-x4plus",        // 真实照片/通用图像（默认）
	"photo":      "realesrgan-x4plus",
	"anime":      "realesrgan-x4plus-anime",  // 动漫插画
	"animevideo": "realesr-animevideov3",     // 动画视频帧，速度快，原生支持 2/3/4 倍
	"video":      "realesr-animevideov3",
}

func main() {
	var (
		input    = flag.String("i", "", "输入图片路径（jpg/png/webp）或包含图片的目录")
		output   = flag.String("o", "", "输出图片路径或目录（目录批量处理时）")
		scale    = flag.Int("s", 4, "放大倍数：2 / 3 / 4")
		model    = flag.String("n", "general", "模型：general（真实照片）| anime（动漫插画）| animevideo（动画/快速），也可直接填引擎模型名")
		tile     = flag.Int("t", 0, "分块大小（0=自动；显存/内存不足时调小，如 128/64）")
		gpu      = flag.Int("g", -1, "GPU 设备 id（默认自动选择）")
		threads  = flag.String("j", "", "加载/处理/保存线程数，如 1:2:2")
		tta      = flag.Bool("x", false, "启用 TTA 模式，画质略高但速度慢数倍")
		format   = flag.String("f", "", "输出格式：jpg / png / webp（默认按输出扩展名）")
		verbose  = flag.Bool("v", false, "打印详细推理日志")
		setup    = flag.Bool("setup", false, "强制重新下载/安装引擎与模型")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `%s — 本地 AI 图片高清修复（Real-ESRGAN）

用法:
  %s -i 输入图片 -o 输出图片 [-s 2|3|4] [-n general|anime|animevideo]

示例:
  %s -i photo.jpg -o photo_4x.png              # 照片放大 4 倍（默认）
  %s -i low.png -o high.png -s 2               # 放大 2 倍
  %s -i frame.png -o out.png -n animevideo     # 动画图像，速度更快
  %s -i ./inputs -o ./outputs                  # 批量处理整个目录

首次运行会自动从 GitHub 下载引擎与模型（约 50MB），之后完全离线运行。

选项:
`, appName, appName, appName, appName, appName, appName)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *input == "" || *output == "" {
		flag.Usage()
		fatal("必须同时指定 -i 输入和 -o 输出")
	}
	if *scale != 2 && *scale != 3 && *scale != 4 {
		fatal("放大倍数 -s 只能是 2、3 或 4")
	}

	dataDir, err := installDir()
	if err != nil {
		fatal(err.Error())
	}
	binPath := filepath.Join(dataDir, engineBinary())
	if *setup || !fileExists(binPath) {
		if err := install(dataDir); err != nil {
			fatal("引擎安装失败: " + err.Error())
		}
	}

	inInfo, inErr := probeSize(*input)

	args := []string{"-i", *input, "-o", *output,
		"-s", fmt.Sprint(*scale),
		"-m", filepath.Join(dataDir, "models"),
		"-n", resolveModel(*model),
		"-t", fmt.Sprint(*tile),
	}
	if *gpu >= 0 {
		args = append(args, "-g", fmt.Sprint(*gpu))
	}
	if *threads != "" {
		args = append(args, "-j", *threads)
	}
	if *format != "" {
		args = append(args, "-f", *format)
	}
	if *tta {
		args = append(args, "-x")
	}
	if *verbose {
		args = append(args, "-v")
	}

	if inErr == nil {
		fmt.Printf("输入: %s (%dx%d)\n", *input, inInfo.w, inInfo.h)
	} else {
		fmt.Printf("输入: %s\n", *input)
	}
	fmt.Printf("模型: %s | 放大: %dx\n", resolveModel(*model), *scale)
	start := time.Now()

	cmd := exec.Command(binPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal("推理失败: " + err.Error())
	}

	fmt.Printf("耗时: %.1fs\n", time.Since(start).Seconds())
	if out, err := probeSize(*output); err == nil {
		fmt.Printf("输出: %s (%dx%d)\n", *output, out.w, out.h)
	} else {
		fmt.Printf("输出: %s\n", *output)
	}
}

func resolveModel(name string) string {
	if m, ok := modelAliases[strings.ToLower(name)]; ok {
		return m
	}
	return name // 允许直接使用引擎原生模型名
}

func engineBinary() string {
	if runtime.GOOS == "windows" {
		return "realesrgan-ncnn-vulkan.exe"
	}
	return "realesrgan-ncnn-vulkan"
}

func installDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "."+appName)
	return dir, os.MkdirAll(dir, 0o755)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// install 下载并解压官方引擎包到 dataDir
func install(dataDir string) error {
	url, ok := pkgURLs[runtime.GOOS]
	if !ok {
		return fmt.Errorf("暂不支持平台: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("首次使用，正在下载 Real-ESRGAN 引擎与模型...\n%s\n", url)
	tmpZip := filepath.Join(dataDir, "pkg.zip")
	if err := download(url, tmpZip); err != nil {
		return err
	}
	defer os.Remove(tmpZip)

	fmt.Println("下载完成，正在解压...")
	if err := unzip(tmpZip, dataDir); err != nil {
		return err
	}

	binPath := filepath.Join(dataDir, engineBinary())
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binPath, 0o755); err != nil {
			return err
		}
	}
	if !fileExists(binPath) {
		return fmt.Errorf("解压后未找到引擎程序 %s", binPath)
	}
	fmt.Println("安装完成。")
	return nil
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回 HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	pw := &progressWriter{total: total, terminal: isTerminal()}
	_, err = io.Copy(io.MultiWriter(f, pw), resp.Body)
	pw.finish()
	return err
}

func isTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type progressWriter struct {
	total     int64
	written   int64
	lastPrint time.Time
	terminal  bool
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	p.written += int64(n)
	// 非终端（输出被重定向/管道）时不打印动态进度，避免刷屏
	if !p.terminal {
		return n, nil
	}
	if time.Since(p.lastPrint) > 300*time.Millisecond {
		p.lastPrint = time.Now()
		if p.total > 0 {
			fmt.Fprintf(os.Stderr, "\r下载进度: %.1f%% (%.1f/%.1f MB)",
				float64(p.written)/float64(p.total)*100,
				float64(p.written)/1024/1024, float64(p.total)/1024/1024)
		} else {
			fmt.Fprintf(os.Stderr, "\r已下载: %.1f MB", float64(p.written)/1024/1024)
		}
	}
	return n, nil
}

func (p *progressWriter) finish() {
	if p.terminal {
		fmt.Fprintln(os.Stderr)
	}
}

func unzip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// 防止 zip slip
		clean := filepath.Clean(f.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("压缩包内含非法路径: %s", f.Name)
		}
		target := filepath.Join(dest, clean)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

type dims struct{ w, h int }

// probeSize 读取图片尺寸；文件不存在（如输出还未生成）时返回错误。
func probeSize(path string) (dims, error) {
	f, err := os.Open(path)
	if err != nil {
		return dims{}, err
	}
	defer f.Close()
	// 仅用标准库，支持 jpg/png；webp 等格式静默跳过
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return dims{}, err
	}
	return dims{cfg.Width, cfg.Height}, nil
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "错误: "+msg)
	os.Exit(1)
}
