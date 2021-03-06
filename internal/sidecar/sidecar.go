package sidecar

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"github.com/reconquest/snake-runner/internal/cloud"
	"github.com/reconquest/snake-runner/internal/sshkey"
)

const (
	SidecarImage = "reconquest/snake-runner-sidecar"
)

const SSHConfigWithoutVerification = `Host *
	StrictHostKeyChecking no
	UserKnownHostsFile /dev/null
`

//go:generate gonstructor -type Sidecar -constructorTypes builder
type Sidecar struct {
	cloud           *cloud.Cloud
	name            string
	pipelinesDir    string
	slug            string
	commandConsumer cloud.CommandConsumer
	outputConsumer  cloud.OutputConsumer
	sshKey          sshkey.Key

	container    *cloud.Container `gonstructor:"-"`
	containerDir string           `gonstructor:"-"`
	hostSubDir   string           `gonstructor:"-"`
}

func (sidecar *Sidecar) GetPipelineVolumes() []string {
	return []string{sidecar.hostSubDir + ":" + sidecar.containerDir}
}

func (sidecar *Sidecar) GetContainerDir() string {
	return sidecar.containerDir
}

func (sidecar *Sidecar) GetContainer() *cloud.Container {
	return sidecar.container
}

func (sidecar *Sidecar) create(ctx context.Context) error {
	img, err := sidecar.cloud.GetImageWithTag(ctx, SidecarImage)
	if err != nil {
		return err
	}

	if img == nil {
		err := sidecar.cloud.PullImage(ctx, SidecarImage, sidecar.outputConsumer)
		if err != nil {
			return karma.Format(
				err,
				"unable to pull sidecar image: %s", SidecarImage,
			)
		}
	}

	sidecar.hostSubDir = filepath.Join(sidecar.pipelinesDir, sidecar.name)
	sidecar.containerDir = filepath.Join("/pipelines/", sidecar.slug)

	volumes := []string{
		sidecar.hostSubDir + ":" + sidecar.containerDir + ":rw",
		sidecar.pipelinesDir + ":/host:rw",
	}

	sidecar.container, err = sidecar.cloud.CreateContainer(
		ctx,
		SidecarImage,
		"snake-runner-sidecar-"+sidecar.name,
		volumes,
	)
	if err != nil {
		return karma.Format(
			err,
			"unable to create sidecar container",
		)
	}

	return nil
}

func (sidecar *Sidecar) Serve(ctx context.Context, cloneURL string, commitish string) error {
	err := sidecar.create(ctx)
	if err != nil {
		return err
	}

	env := []string{
		"__SNAKE_PRIVATE_KEY=" + string(sidecar.sshKey.Private),
		"__SNAKE_PUBLIC_KEY=" + string(sidecar.sshKey.Public),
		"__SNAKE_SSH_CONFIG=" + SSHConfigWithoutVerification,
	}

	basic := []string{
		`mkdir ~/.ssh`,
		`cat > ~/.ssh/id_rsa <<< "$__SNAKE_PRIVATE_KEY"`,
		`cat > ~/.ssh/id_rsa.pub <<< "$__SNAKE_PUBLIC_KEY"`,
		`cat > ~/.ssh/config <<< "$__SNAKE_SSH_CONFIG"`,
		`chmod 0600 ~/.ssh/id_rsa ~/.ssh/id_rsa.pub`,
		`git config --global advice.detachedHead false`,
	}

	cmd := []string{"bash", "-c", strings.Join(basic, " && ")}

	err = sidecar.cloud.Exec(ctx, sidecar.container, types.ExecConfig{
		Cmd:          cmd,
		Env:          env,
		AttachStdout: true,
		AttachStderr: true,
	}, sidecar.onlyLog)
	if err != nil {
		return karma.Describe("cmd", cmd).Format(
			err,
			"unable to prepare sidecar container",
		)
	}

	commands := [][]string{
		{`git`, `clone`, cloneURL, sidecar.containerDir},
		{`git`, `-C`, sidecar.containerDir, `checkout`, commitish},
	}

	for _, cmd := range commands {
		sidecar.commandConsumer(cmd)

		err = sidecar.cloud.Exec(ctx, sidecar.container, types.ExecConfig{
			// NO ENV!
			Cmd:          cmd,
			AttachStdout: true,
			AttachStderr: true,
		}, sidecar.outputConsumer)
		if err != nil {
			return karma.
				Describe("cmd", cmd).
				Format(err, "unable to setup repository")
		}
	}

	return nil
}

func (sidecar *Sidecar) onlyLog(text string) {
	log.Debugf(
		nil,
		"sidecar %s: %s",
		sidecar.container.Name, strings.TrimRight(text, "\n"),
	)
}

func (sidecar *Sidecar) Destroy() {
	if sidecar.container == nil {
		return
	}
	// we use Background context here because local ctx can be destroyed
	// already

	if sidecar.name != "" {
		cmd := []string{"rm", "-rf", filepath.Join("/host", sidecar.name)}

		log.Debugf(
			nil,
			"cleaning up sidecar %s container: %v",
			sidecar.container.Name, cmd,
		)

		err := sidecar.cloud.Exec(
			context.Background(),
			sidecar.container,
			types.ExecConfig{Cmd: cmd, AttachStderr: true, AttachStdout: true},
			sidecar.onlyLog,
		)
		if err != nil {
			log.Errorf(
				err,
				"unable to cleanup sidecar directory: %s %s",
				sidecar.GetContainerDir(),
				sidecar.hostSubDir,
			)
		}
	}

	log.Debugf(
		nil,
		"destroying sidecar %s container",
		sidecar.container.Name,
	)

	err := sidecar.cloud.DestroyContainer(context.Background(), sidecar.container)
	if err != nil {
		log.Errorf(
			err,
			"unable to destroy sidecar container: %s %s",
			sidecar.container.ID,
			sidecar.container.Name,
		)
	}
}
