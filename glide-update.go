package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/xanzy/go-gitlab"
	"gopkg.in/yaml.v2"
)

// Owner describes an owner of a package. This can be a person, company, or
// other organization. This is useful if someone needs to contact the
// owner of a package to address things like a security issue.
type Owner struct {
	// Name describes the name of an organization.
	Name string `yaml:"name,omitempty"`

	// Email is an email address to reach the owner at.
	Email string `yaml:"email,omitempty"`

	// Home is a url to a website for the owner.
	Home string `yaml:"homepage,omitempty"`
}

// Owners is a list of owners for a project.
type Owners []*Owner

// Dependency describes a package that the present package depends upon.
type Dependency struct {
	Name        string   `yaml:"package"`
	Reference   string   `yaml:"version,omitempty"`
	Pin         string   `yaml:"-"`
	Repository  string   `yaml:"repo,omitempty"`
	VcsType     string   `yaml:"vcs,omitempty"`
	Subpackages []string `yaml:"subpackages,omitempty"`
	Arch        []string `yaml:"arch,omitempty"`
	Os          []string `yaml:"os,omitempty"`
}

// Dependencies is a collection of Dependency
type Dependencies []*Dependency

// Config is a transitive representation of a dependency for importing and exporting to yaml.
type Config struct {
	Name        string       `yaml:"package"`
	Description string       `yaml:"description,omitempty"`
	Home        string       `yaml:"homepage,omitempty"`
	License     string       `yaml:"license,omitempty"`
	Owners      Owners       `yaml:"owners,omitempty"`
	Ignore      []string     `yaml:"ignore,omitempty"`
	Exclude     []string     `yaml:"excludeDirs,omitempty"`
	Imports     Dependencies `yaml:"import"`
	DevImports  Dependencies `yaml:"testImport,omitempty"`
}

// Lock represents an individual locked dependency.
type Lock struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Repository  string   `yaml:"repo,omitempty"`
	VcsType     string   `yaml:"vcs,omitempty"`
	Subpackages []string `yaml:"subpackages,omitempty"`
	Arch        []string `yaml:"arch,omitempty"`
	Os          []string `yaml:"os,omitempty"`
}

// Locks is a slice of locked dependencies.
type Locks []*Lock

// Lockfile represents a glide.lock file.
type Lockfile struct {
	Hash       string    `yaml:"hash"`
	Updated    time.Time `yaml:"updated"`
	Imports    Locks     `yaml:"imports"`
	DevImports Locks     `yaml:"testImports"`
}

func checkIfError(e error, msg ...string) {
	if e != nil {
		if len(msg) > 0 {
			fmt.Print(msg[0])
		}
		panic(e)
	}
}

const (
	glideYamlName string = "glide.yaml"
	glideNewName  string = "glide.new"
	glideLockName string = "glide.lock"
)

var (
	out []byte
)

// getGroupID returns the groupID
func getGroupID(client *gitlab.Client, groupName string) (groupID int, err error) {

	listGroupOpts := &gitlab.ListGroupsOptions{
		Search: gitlab.String(groupName),
	}

	groups, _, err := client.Groups.ListGroups(listGroupOpts)

	if err != nil {
		return 0, err
	}

	if len(groups) != 1 {
		return 0, errors.New("can't find the specified group")
	}

	return groups[0].ID, nil
}

// listGroupProjects returns map with projectName and their ID
func listGroupProjects(client *gitlab.Client, groupID int) (listProjects map[string]string, err error) {

	options := &gitlab.ListGroupProjectsOptions{ListOptions: gitlab.ListOptions{}}

	listProjects = make(map[string]string)
	for {
		projects, res, err := client.Groups.ListGroupProjects(groupID, options)
		if err != nil {
			return listProjects, err
		}

		for _, project := range projects {
			listProjects[project.Name] = project.HTTPURLToRepo
		}

		if res.NextPage == 0 {
			break
		}
		options.ListOptions.Page = res.NextPage
	}

	return listProjects, nil
}

// createGroupProject returns the projectURL
func createGroupProject(client *gitlab.Client, projectName string, groupID int) (projectURL string, err error) {

	project, _, err := client.Projects.CreateProject(&gitlab.CreateProjectOptions{
		Name:        gitlab.String(projectName),
		NamespaceID: gitlab.Int(groupID),
		Visibility:  gitlab.Visibility(gitlab.InternalVisibility),
	})
	if err != nil {
		return "", err
	}
	return project.HTTPURLToRepo, nil
}

func processGlideCache(client *gitlab.Client, name string, projects map[string]string, glideCachePath string, groupID int) string {

	log.Printf("- processing '%s'", name)

	reg, err := regexp.Compile("[/]+")
	checkIfError(err)
	// repository name on the filesystem
	repoName := reg.ReplaceAllString(name, "-")

	reg, err = regexp.Compile("[/.]+")
	checkIfError(err)
	// repository name on the server
	safeRepoName := reg.ReplaceAllString(name, "-")

	remoteURL, projectExists := projects[safeRepoName]

	localCacheRepo := glideCachePath + repoName
	if _, err := os.Stat(localCacheRepo); err == nil {
		log.Printf("- found local cache repo '%s'", localCacheRepo)

		if projectExists {
			log.Printf("- remote repo '%s' already exists", remoteURL)
		} else {
			remoteURL, err = createGroupProject(client, safeRepoName, groupID)
			if err == nil {
				log.Printf("- remote repo '%s' created", remoteURL)
			}
		}

		out, err = exec.Command("git", "-C", localCacheRepo, "remote", "remove", "upstream").CombinedOutput()
		out, err = exec.Command("git", "-C", localCacheRepo, "remote", "add", "upstream", remoteURL).CombinedOutput()
		checkIfError(err, string(out))

		out, err = exec.Command("git", "-C", localCacheRepo, "push", "--all", "upstream").CombinedOutput()
		checkIfError(err, string(out))
		log.Printf("- push all branches:\n%s", string(out))

		out, err = exec.Command("git", "-C", localCacheRepo, "push", "--tags", "upstream").CombinedOutput()
		checkIfError(err, string(out))
		log.Printf("- push all tags:\n%s", string(out))

		log.Printf("- updated with upstream: '%s'", remoteURL)
		log.Printf("")

		return remoteURL
	}
	return ""
}

func main() {

	cmdArgs := os.Args
	if len(cmdArgs) == 1 {
		log.Printf("Usage: %s <GitLabURL> <GitLabGroupName> <GitLabPrivateToken>", cmdArgs[0])
		os.Exit(1)
	}

	gitLabPrivateToken := cmdArgs[3]
	gitLabGroup := cmdArgs[2]
	gitLabURL := cmdArgs[1]

	gitLabClient := gitlab.NewClient(nil, gitLabPrivateToken)
	gitLabClient.SetBaseURL(gitLabURL + "/api/v3")

	gitLabGroupID, err := getGroupID(gitLabClient, gitLabGroup)
	if err != nil {
		panic(err)
	}

	gitLabProjects, err := listGroupProjects(gitLabClient, gitLabGroupID)
	if err != nil {
		panic(err)
	}

	home := os.Getenv("HOME")
	if //noinspection GoBoolExpressions
	runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	}
	glideCachePath := home + "/.glide/cache/src/https-"

	var glideConfig Config
	var ignoreBlock []string

	// clear glide cache
	out, err = exec.Command("glide", "--no-color", "cache-clear").CombinedOutput()
	checkIfError(err, string(out))
	log.Printf("Executing 'glide cache-clear':\n%s", string(out))

	// remove new glide config
	log.Printf("Removing '%s' file if exists...", glideNewName)
	os.Remove(glideNewName)

	// create new glide config from sources
	out, err = exec.Command("glide", "--no-color", "--yaml", glideNewName, "init", "--non-interactive").CombinedOutput()
	checkIfError(err, string(out))
	log.Printf("Executing 'glide --yaml %s init --non-interactive' ...\n%s", glideNewName, string(out))

	// read created glide config
	log.Printf("Reading '%s' file...", glideNewName)
	glideYaml, err := ioutil.ReadFile(glideNewName)
	checkIfError(err)

	log.Printf("Parsing newly created file...")
	err = yaml.Unmarshal(glideYaml, &glideConfig)
	checkIfError(err)

	// remove own reference from the imports block
	newImports := Dependencies{}
	for _, pkg := range glideConfig.Imports {
		if strings.HasPrefix(pkg.Name, glideConfig.Name) {
			ignoreBlock = append(ignoreBlock, pkg.Name)
		} else {
			newImport := Dependency{
				Name:        pkg.Name,
				Subpackages: pkg.Subpackages,
			}
			newImports = append(newImports, &newImport)
		}
	}
	glideConfig.Imports = newImports

	// remove own reference from the devImports block
	devImports := Dependencies{}
	for _, pkg := range glideConfig.DevImports {
		if strings.HasPrefix(pkg.Name, glideConfig.Name) {
			ignoreBlock = append(ignoreBlock, pkg.Name)
		} else {
			devImport := Dependency{
				Name:        pkg.Name,
				Subpackages: pkg.Subpackages,
			}
			devImports = append(devImports, &devImport)
		}
	}
	glideConfig.DevImports = devImports

	// ignored imports
	glideConfig.Ignore = ignoreBlock

	// create new glide config file
	out, err = yaml.Marshal(&glideConfig)
	checkIfError(err)
	err = ioutil.WriteFile(glideYamlName, out, 0644)
	checkIfError(err)

	log.Printf("Recreated '%s' file...", glideYamlName)

	// remove new glide config
	log.Printf("Removing '%s' file...", glideNewName)
	os.Remove(glideNewName)

	// remove glide lock file
	log.Printf("Removing '%s' file if exists...", glideLockName)
	os.Remove(glideLockName)

	// remove vendor directory with contents
	log.Printf("Purging 'vendor' directory...")
	os.RemoveAll("vendor")

	// import packages with glide
	out, err = exec.Command("glide", "--no-color", "--debug", "install", "--strip-vendor").CombinedOutput()
	checkIfError(err, string(out))
	log.Printf("Executing 'glide install --strip-vendor' ...\n%s", string(out))

	log.Printf("Reading '%s' file...", glideLockName)
	glideLock, err := ioutil.ReadFile(glideLockName)
	checkIfError(err)

	var locks Lockfile
	log.Printf("Parsing file...")
	err = yaml.Unmarshal(glideLock, &locks)
	checkIfError(err)

	glideConfig.Imports = Dependencies{}
	for _, pkg := range locks.Imports {
		remoteURL := processGlideCache(gitLabClient, pkg.Name, gitLabProjects, glideCachePath, gitLabGroupID)
		if remoteURL != "" {
			dep := Dependency{
				Name:        pkg.Name,
				Reference:   pkg.Version,
				Repository:  remoteURL,
				Subpackages: pkg.Subpackages,
			}
			glideConfig.Imports = append(glideConfig.Imports, &dep)
		}
	}
	imported := len(glideConfig.Imports)

	glideConfig.DevImports = Dependencies{}
	for _, pkg := range locks.DevImports {
		remoteURL := processGlideCache(gitLabClient, pkg.Name, gitLabProjects, glideCachePath, gitLabGroupID)
		if remoteURL != "" {
			dep := Dependency{
				Name:        pkg.Name,
				Reference:   pkg.Version,
				Repository:  remoteURL,
				Subpackages: pkg.Subpackages,
			}
			glideConfig.DevImports = append(glideConfig.DevImports, &dep)
		}
	}
	imported = imported + len(glideConfig.DevImports)

	out, err = yaml.Marshal(&glideConfig)
	checkIfError(err)
	err = ioutil.WriteFile(glideYamlName, out, 0644)
	checkIfError(err)

	log.Printf("Created %s with %d repos.", glideYamlName, imported)
}
