# glide-update #

**glide-update** is [Glide](https://github.com/Masterminds/glide) package management Utility for creating a local mirrors.

**glide-update** work with any [GitLab](https://gitlab.com) using v3 API.

## Install (GoLang Way) ##

```bash
go get github.com/mmalchuk/glide-update
```

## Compile localy ##

```bash
go get github.com/xanzy/go-gitlab
go get gopkg.in/yaml.v2
go build
```

## Usage ##

```bash
glide-update <GitLabURL> <GitLabGroupName> <GitLabPrivateToken>
```

for example:

```bash
glide-update https://gitlab.com glide-mirror YoUrPriVateToKen
```

would mirror:

* 'http://gopkg.in/yaml.v2' to the 'https://gitlab.com/glide-mirror/gopkg-in-yaml-v2'
* 'https://github.com/xanzy/go-gitlab' to the 'https://gitlab.com/glide-mirror/github-com-xanzy-go-gitlab'
* etc...

then update local [glide.yaml](glide.yaml) file with mirrors.

## License ##

This Utility is distributed under the MIT-style license found in the [LICENSE](./LICENSE) file.
