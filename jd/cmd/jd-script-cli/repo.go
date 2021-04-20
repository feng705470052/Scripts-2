package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"jd_scripts/pkg"
)

const (
	SelectTypePullGitRepo = iota
	SelectTypeSpiderOtherScript
	SelectTypeGenerateJdScriptShell
)

var gitAuthorRepoMap = map[string]string{
	"yangtingxiao": "https://github.com/yangtingxiao/QuantumultX.git",
}

var gitAuthorPathMap = map[string][]string{
	"i-chenzhe":    {"i-chenzhe"},
	"monk-coder":   {"car", "member", "normal"},
	"yangtingxiao": {"scripts"},
}

// 这里希望写入 [*(所有文件) | @脚本名字(过滤脚本) | 指定脚本名字] 三种方式
var gitAuthorScripts = map[string][]string{
	"i-chenzhe":    {"@z_getFanslove.js"},
	"monk-coder":   {"@monk_skyworth.js"},
	"yangtingxiao": {"jd_lotteryMachine.js"},
}

const (
	cronRegex   = `(((\*|\?|[0-9]{1,2}|[0-9]{1,2}\-[0-9]{1,2}|[0-9]{1,2}\-[0-9]{1,2}\/[0-9]{1,2}|([0-9]{1,2}\,?)*|([0-9]{1,2}\,?)*\-[0-9]{1,2}|([0-9]{1,2}\,?)*\-[0-9]{1,2}\/[0-9]{1,2})+[\s]){5})`
	activeRegex = `(?m)new Env\(\"?\'?(.*?)\"?\'?\)`
)

func GetScriptTemplate() string {
	return `#!/bin/bash

function initGitRepo() {
   git clone https://gitee.com/yqchilde/Scripts.git /ybRepo
}

if [ ! -d "/ybRepo/" ]; then
   echo "未检查到ybRepo仓库脚本，初始化下载相关脚本"
   initGitRepo
else
   echo "更新ybRepo脚本相关文件"
   git -C /ybRepo reset --hard
   git -C /ybRepo pull --rebase
fi

cp $(find /ybRepo/jd/scripts/author -type f -name "*.js") /scripts/

{
  {{- range $_, $cron := .CronList}}
  {{ $cron -}}
  {{- end }}
} >> /scripts/docker/merged_list_file.sh
`
}

// GenerateJDScriptShell 生成jd_script.sh脚本
func GenerateJDScriptShell() {
	var cronList []string

	for _, author := range []string{"i-chenzhe", "monk-coder", "yangtingxiao"} {
		pkg.Info("遍历当前 %s 的脚本并生成对应的cron", author)
		currentScripts, _ := filepath.Glob("./scripts/author/" + author + "/*.js")
		for i := range currentScripts {
			_, fileName := filepath.Split(currentScripts[i])
			fileName = strings.ReplaceAll(fileName, ".js", "")
			cron, active := "", ""
			if file, err := os.Open(currentScripts[i]); err != nil {
				panic(err)
			} else {
				scanner := bufio.NewScanner(file)
				cronReg := regexp.MustCompile(cronRegex)
				activeReg := regexp.MustCompile(activeRegex)
				for scanner.Scan() {
					if cronReg.MatchString(scanner.Text()) {
						cron = strings.Trim(cronReg.FindString(scanner.Text()), " ")
					}
					if activeReg.MatchString(scanner.Text()) {
						active = strings.Trim(activeReg.FindStringSubmatch(scanner.Text())[1], " ")
					}

					if cron != "" && active != "" {
						break
					}
				}
			}

			cronList = append(cronList, `printf "# `+active+`\n`+cron+` node /scripts/`+fileName+`.js >> /scripts/logs/`+fileName+`.log 2>&1\n"`)
		}
	}

	// 先写入文件然后覆盖
	if len(cronList) != 0 {
		pkg.Info("重新写入jd_script.sh")
		type Cron struct {
			CronList []string
		}

		var buf bytes.Buffer
		cron := &Cron{CronList: cronList}
		t := template.Must(template.New("jd_script").Parse(GetScriptTemplate()))
		err := t.Execute(&buf, cron)
		if err != nil {
			pkg.Warning("Executing template:", err)
		}

		if err = ioutil.WriteFile("./jd_script.sh", buf.Bytes(), 0644); err != nil {
			pkg.CheckIfError(err)
		}
	}
}

// GitCloneRepo ...
func GitCloneRepo() {
	for _, author := range []string{"yangtingxiao"} {
		pkg.Info("正在处理 %s 的脚本", author)

		hasGitPath := pkg.CheckFileExists(author)
		if !hasGitPath {
			_, err := CloneScriptRepo(gitAuthorRepoMap[author], author, "master")
			pkg.CheckIfError(err)
		} else {
			ret, err := PullScriptRepo(author)
			pkg.CheckIfError(err)
			if strings.Contains(ret, "Already up to date") {
				pkg.Warning("%s 的仓库没有更新，即将跳过", author)
				continue
			}
		}

		// 移除旧文件
		pkg.Info("移除旧文件")
		_, err := pkg.CopyFile("scripts/author/"+author, "scripts/backup/"+author)
		pkg.CheckIfError(err)

		// 开始拷贝文件
		pkg.Info("开始拷贝 %s 脚本", author)

		scriptPaths := gitAuthorPathMap[author]
		scriptFiles := gitAuthorScripts[author]
		var scriptFilePaths []string
		for i := range scriptPaths {
			if err := filepath.Walk(author+"/"+scriptPaths[i], func(path string, info os.FileInfo, err error) error {
				if len(scriptFiles) == 1 && scriptFiles[0] == "*" {
					if filepath.Ext(path) == ".js" {
						scriptFilePaths = append(scriptFilePaths, path)
					}
				} else if scriptFiles[0][0] == '@' {
					var isMatch bool
					for k := range scriptFiles {
						if scriptFiles[k][0] != '@' {
							pkg.Warning("%s 的脚本过滤文件规则不一致", k)
							return nil
						}

						filterScriptName := scriptFiles[k][1:]
						if info.Name() == filterScriptName {
							isMatch = true
							break
						}
					}

					if !isMatch && filepath.Ext(path) == ".js" {
						scriptFilePaths = append(scriptFilePaths, path)
					}
				} else {
					for j := range scriptFiles {
						if info.Name() == scriptFiles[j] {
							fmt.Println(path)
							scriptFilePaths = append(scriptFilePaths, path)
						}
					}
				}
				return nil
			}); err != nil {
				pkg.CheckIfError(err)
			}
		}

		// 将文件移到指定项目目录
		pkg.Info("将 %s 脚本移到指定项目目录", author)
		for i := range scriptFilePaths {
			_, fileName := filepath.Split(scriptFilePaths[i])
			exists := pkg.CheckFileExists(scriptFilePaths[i])
			if exists {
				_, err := pkg.CopyFile(scriptFilePaths[i], "./scripts/author/"+author+"/"+fileName)
				pkg.CheckIfError(err)
			}
		}
	}
}