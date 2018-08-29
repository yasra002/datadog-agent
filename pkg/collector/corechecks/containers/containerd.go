package containers

import (
	"context"
	"fmt"
	"github.com/DataDog/datadog-agent/pkg/aggregator"
	"github.com/DataDog/datadog-agent/pkg/autodiscovery/integration"
	"github.com/DataDog/datadog-agent/pkg/collector/check"
	"github.com/DataDog/datadog-agent/pkg/collector/corechecks"
	core "github.com/DataDog/datadog-agent/pkg/collector/corechecks"
	"github.com/DataDog/datadog-agent/pkg/tagger"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/containerd/cgroups"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl"
	"github.com/gogo/protobuf/types"
	"gopkg.in/yaml.v2"
)

type ContainerdCheck struct {
	core.CheckBase
	instance *ContainerdConfig
	cl       containerd.Client
}

type ContainerdConfig struct {
	Tags             []string
	CollectImageSize bool `yaml:"collect_image_size"`
}

func init() {
	corechecks.RegisterCheck("containerd", ContainerdFactory)
}

func ContainerdFactory() check.Check {
	client, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		log.Errorf("Can't connect to containerd socket: %v", err.Error())
		return nil
	}
	return &ContainerdCheck{
		CheckBase: corechecks.NewCheckBase("containerd"),
		instance:  &ContainerdConfig{},
		cl:        *client,
	}
}

func (c *ContainerdCheck) Run() error {
	sender, err := aggregator.GetSender(c.ID())
	if err != nil {
		return err
	}
	defer sender.Commit()
	nsList, err := c.cl.NamespaceService().List(context.Background())
	if err != nil {
		return err
	}
	for _, n := range nsList {
		c.computeMetrics(sender, n)
	}
	return nil
}

func (c *ContainerdCheck) convertTasktoMetrics(task containerd.Task, nsCtx context.Context) (*cgroups.Metrics, error) {

	metricTask, err := task.Metrics(nsCtx)
	if err != nil {
		return nil, err
	}
	metricAny, err := typeurl.UnmarshalAny(&types.Any{
		TypeUrl: metricTask.Data.TypeUrl,
		Value:   metricTask.Data.Value,
	})
	if err != nil {
		log.Errorf(err.Error())
		return nil, err
	}

	return metricAny.(*cgroups.Metrics), nil
}

func (c *ContainerdCheck) computeMetrics(sender aggregator.Sender, ns string) {
	nk := namespaces.WithNamespace(context.Background(), ns)

	containers, err := c.cl.Containers(nk)
	if err != nil {
		log.Errorf(err.Error())
		return
	}

	for _, ctn := range containers {

		t, err := ctn.Task(nk, nil)
		if err != nil {
			log.Errorf(err.Error())
			continue
		}

		tags, err := c.collectTags(ctn, nk)
		if err != nil {
			log.Errorf("Could not collect tags for container %s: %s", ctn.ID()[:12], err)
		}

		metrics, err := c.convertTasktoMetrics(t, nk)
		if err != nil {
			log.Errorf("Could not process the metrics from %s: %v", ctn.ID(), err.Error())
			continue
		}

		err = c.computeExtra(sender, ctn, nk, tags)
		if err != nil {
			log.Errorf("Could not process metadata related metrics for %s: %v", ctn.ID()[:12], err.Error())
		}

		err = computeCache(sender, metrics.Memory, tags)
		if err != nil {
			log.Errorf("Could not process memory related metrics for %s: %v", ctn.ID()[:12], err.Error())
		}

		err = computeCPU(sender, metrics.CPU, tags)
		if err != nil {
			log.Errorf("Could not process cpu related metrics for %s: %v", ctn.ID()[:12], err.Error())
		}

		err = computeBlkio(sender, metrics.Blkio, tags)
		if err != nil {
			log.Errorf("Could not process blkio related metrics for %s: %v", ctn.ID()[:12], err.Error())
		}

		err = computeHugetlb(sender, metrics.Hugetlb, tags)
		if err != nil {
			log.Errorf("Could not process hugetlb related metrics for %s: %v", ctn.ID()[:12], err.Error())
		}
	}
}

func (c *ContainerdCheck) computeExtra(sender aggregator.Sender, ctn containerd.Container, nsCtx context.Context, tags []string) error {
	if c.instance.CollectImageSize {
		img, err := ctn.Image(nsCtx)
		if err != nil {
			return err
		}
		size, err := img.Size(nsCtx)
		if err != nil {
			return err
		}
		sender.Rate("containerd.image.size", float64(size), "", tags)
	}
	return nil
}

func (c *ContainerdCheck) collectTags(ctn containerd.Container, nsCtx context.Context) ([]string, error) {
	tags := []string{}
	// Container image
	im, err := ctn.Image(nsCtx)
	if err != nil {
		return tags, err
	}
	imageName := fmt.Sprintf("image:", im.Name())
	tags = append(tags, imageName)

	// Container labels
	labels, err := ctn.Labels(nsCtx)
	if err != nil {
		return tags, err
	}
	for k, v := range labels {
		tag := fmt.Sprintf("%s:%s", k, v)
		tags = append(tags, tag)
	}

	// Container meta
	i, err := ctn.Info(nsCtx)
	if err != nil {
		return tags, err
	}
	runt := fmt.Sprintf("runtime:%s", i.Runtime.Name)
	tags = append(tags, runt)

	// Tagger tags
	taggerTags, err := tagger.Tag(ctn.ID(), true)
	if err != nil {
		return tags, err
	}
	tags = append(tags, taggerTags...)

	// User tags
	tags = append(tags, c.instance.Tags...)
	return tags, nil
}

func computeHugetlb(sender aggregator.Sender, huge []*cgroups.HugetlbStat, tags []string) error {
	if huge == nil {
		return fmt.Errorf("no hugetbl metrics available")
	}
	for _, h := range huge {
		sender.Gauge("containerd.hugetlb.", float64(h.Max), "", tags)
		sender.Gauge("containerd.hugetlb.", float64(h.Failcnt), "", tags)
		sender.Gauge("containerd.hugetlb.", float64(h.Usage), "", tags)
	}
	return nil
}

func computeCache(sender aggregator.Sender, mem *cgroups.MemoryStat, tags []string) error {
	if mem == nil {
		return fmt.Errorf("no cpu metrics available")
	}
	memList := map[string]*cgroups.MemoryEntry{
		"containerd.mem.current":    mem.Usage,
		"containerd.mem.kernel_tcp": mem.KernelTCP,
		"containerd.mem.kernel":     mem.Kernel,
		"containerd.mem.swap":       mem.Swap,
	}
	for metricName, memStat := range memList {
		parseAndSubmitMem(metricName, sender, memStat, tags)
	}
	sender.Rate("containerd.mem.cache", float64(mem.Cache), "", tags)
	sender.Rate("containerd.mem.rss", float64(mem.RSS), "", tags)
	sender.Rate("containerd.mem.usage", float64(mem.Usage.Usage), "", tags)
	sender.Rate("containerd.mem.kernel.usage", float64(mem.Kernel.Usage), "", tags)
	sender.Rate("containerd.mem.dirty", float64(mem.Dirty), "", tags)
	sender.Rate("containerd.mem.swap", float64(mem.Swap.Usage), "", tags)
	return nil
}

func parseAndSubmitMem(metricName string, sender aggregator.Sender, stat *cgroups.MemoryEntry, tags []string) {
	if stat.Size() == 0 {
		return
	}
	sender.Gauge(fmt.Sprintf("%s.usage", metricName), float64(stat.Usage), "", tags)
	sender.Gauge(fmt.Sprintf("%s.failcnt", metricName), float64(stat.Failcnt), "", tags)
	sender.Gauge(fmt.Sprintf("%s.limit", metricName), float64(stat.Limit), "", tags)
	sender.Gauge(fmt.Sprintf("%s.max", metricName), float64(stat.Max), "", tags)

}

func computeCPU(sender aggregator.Sender, cpu *cgroups.CPUStat, tags []string) error {
	if cpu.Throttling == nil || cpu.Usage == nil {
		return fmt.Errorf("no cpu metrics available")
	}
	sender.Rate("containerd.cpu.system", float64(cpu.Usage.Kernel), "", tags)
	sender.Rate("containerd.cpu.total", float64(cpu.Usage.Total), "", tags)
	sender.Rate("containerd.cpu.user", float64(cpu.Usage.User), "", tags)
	sender.Rate("containerd.throttle.periods", float64(cpu.Throttling.Periods), "", tags)

	return nil
}

func computeBlkio(sender aggregator.Sender, blkio *cgroups.BlkIOStat, tags []string) error {
	if blkio.Size() == 0 {
		return fmt.Errorf("no blkio metrics available")
	}
	blkioList := map[string][]*cgroups.BlkIOEntry{
		"containerd.blkio.serviced_recursive":      blkio.IoServicedRecursive,
		"containerd.blkio.service_recursive_bytes": blkio.IoServiceBytesRecursive,
		"containerd.blkio.queued_recursive":        blkio.IoQueuedRecursive,
		"containerd.blkio.sectors_recursive":       blkio.SectorsRecursive,
		"containerd.blkio.merged_recursive":        blkio.IoMergedRecursive,
		"containerd.blkio.service_time_recursive":  blkio.IoServiceTimeRecursive,
		"containerd.blkio.time_recursive":          blkio.IoTimeRecursive,
		"containerd.blkio.wait_time_recursive":     blkio.IoWaitTimeRecursive,
	}
	for metricName, ioStats := range blkioList {
		parseAndSubmitBlkio(metricName, sender, ioStats, tags)
	}
	return nil
}

func parseAndSubmitBlkio(metricName string, sender aggregator.Sender, list []*cgroups.BlkIOEntry, tags []string) {

	for _, m := range list {
		if m.Size() == 0 {
			continue
		}
		blkiotags := []string{
			fmt.Sprintf("dev:%s", m.Device),
		}
		if m.Op != "" {
			blkiotags = append(blkiotags, fmt.Sprintf("operation:%s", m.Op))
		}

		tags = append(tags, blkiotags...)
		sender.Rate(metricName, float64(m.Value), "", tags)
	}
}

func (co *ContainerdConfig) Parse(data []byte) error {
	co.CollectImageSize = true
	if err := yaml.Unmarshal(data, co); err != nil {
		return err
	}
	return nil
}

func (c *ContainerdCheck) Configure(config, initConfig integration.Data) error {
	c.instance.Parse(config)

	return nil
}

func (c *ContainerdCheck) Stop() {
	c.cl.Close()
}
