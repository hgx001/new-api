package manwu

import (
	"os"
)

// readFileForTest 读取仓库内文件（测试用，路径相对本包目录）。
func readFileForTest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
