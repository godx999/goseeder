package qbittorrent

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"seeder/src/config"
	"seeder/src/datebase"
	"seeder/src/qbittorrent/pkg/model"
	"strings"
	"time"
)

type ServerStatus struct {
	FreeSpaceOnDisk    int
	EstimatedQuota     int
	ConcurrentDownload int
	UpInfoSpeed        int
	DownInfoSpeed      int
}

type Server struct {
	Client *Client
	Rule   config.ServerRule
	Remark string
	Status ServerStatus
}

func (s *Server) ServerClean(cfg config.Config, db datebase.Client) {
	//开始执行删除操作
	if s.Status.FreeSpaceOnDisk < s.Rule.DiskThreshold {
		var options model.GetTorrentListOptions
		options.Filter = "all"
		if ts, err := s.Client.Torrent.GetList(&options); err == nil {
			for _, t := range ts {
				for _, n := range cfg.Node {
					if n.Source == t.Category {
						if trackers, err := s.Client.Torrent.GetTrackers(t.Hash); err == nil && (int(time.Now().Unix())-t.AddedOn) > s.Rule.MinAliveTime {
							for _, tracker := range trackers {
								if tracker.Status == model.TrackerStatusNotContacted || tracker.Status == model.TrackerStatusNotWorking {
									s.Client.Torrent.DeleteTorrents([]string{t.Hash}, true)
									fmt.Println("清理无效种子." + t.Name)
								}
							}
						}

						if t.AmountLeft == 0 {
							if t.Upspeed == 0 && t.AmountLeft == 0 {
								if (int(time.Now().Unix())-t.CompletionOn) > n.Rule.SeederTime || t.Ratio > n.Rule.SeederRatio {
									err = s.Client.Torrent.DeleteTorrents([]string{t.Hash}, true)
									fmt.Println(err)
									db.MarkFinished(t.Hash)
									fmt.Println("标记完成种子." + t.Name)
								}
							}
						} else {
							if (int(time.Now().Unix()) - t.CompletionOn) > s.Rule.MaxAliveTime {
								s.Client.Torrent.DeleteTorrents([]string{t.Hash}, true)
								fmt.Println("删除超时种子." + t.Name)
							}
						}
					}
				}
			}
		}
	}
}

func (s *Server) ServerRuleTest() bool {
	if s.Status.UpInfoSpeed > s.Rule.MaxSpeed {
		fmt.Printf(s.Remark + "上传速度过快,规则测试失败,限制速度 %.2f MB,现在速度 %.2f MB \n",float64(s.Rule.MaxSpeed) / 1048576.0,float64(s.Status.UpInfoSpeed) / 1048576.0)
		return false
	}

	if s.Status.DownInfoSpeed > s.Rule.MaxSpeed {
		fmt.Printf(s.Remark + "下载速度过快,规则测试失败,限制速度 %.2f MB,现在速度 %.2f MB \n",float64(s.Rule.MaxSpeed) / 1048576.0,float64(s.Status.DownInfoSpeed) / 1048576.0)
		return false
	}

	if s.Status.ConcurrentDownload > s.Rule.ConcurrentDownload {
		fmt.Printf(s.Remark + "同时任务数过多,规则测试失败,限制个数 %d,现在个数 %d \n",s.Rule.ConcurrentDownload,s.Status.ConcurrentDownload)
		return false
	}

	fmt.Printf(s.Remark + "规则测试通过.\n")

	return true
}

func (s *Server) AddTorrentByURL(URL string, Size int) bool {
	var options model.AddTorrentsOptions
	options.Savepath = "/downloads/"
	options.Category = strings.Split(strings.Split(URL, "//")[1], "/")[0]

	if Size < s.Rule.MaxTaskSize && Size > s.Rule.MinTaskSize && s.ServerRuleTest() == true {
		if err := s.Client.Torrent.AddURLs([]string{URL}, &options); err == nil {
			return true
		}
	}

	return false
}

func (s *Server) CalcEstimatedQuota() {
	// 这里计算出来的是磁盘正在可以用的空间
	if r, err := s.Client.Sync.GetMainData(); err == nil {
		s.Status.FreeSpaceOnDisk = r.ServerState.FreeSpaceOnDisk
		s.Status.EstimatedQuota = r.ServerState.FreeSpaceOnDisk
		// 这里计算出来的是磁盘预期可以用的空间.(假设种子会全部下载)
		var options model.GetTorrentListOptions
		options.Filter = "all"
		if ts, err := s.Client.Torrent.GetList(&options); err == nil {
			s.Status.ConcurrentDownload = 0
			for _, t := range ts {
				if t.AmountLeft != 0 {
					s.Status.ConcurrentDownload++
				}
				s.Status.EstimatedQuota -= t.AmountLeft
			}
		}
	}else{
		fmt.Println(err)
	}

	if r, err := s.Client.Transfer.GetTransferInfo(); err == nil {
		s.Status.UpInfoSpeed = r.UpInfoSpeed
		s.Status.DownInfoSpeed = r.DlInfoSpeed
	}

	fmt.Printf("[%s]当前磁盘空间余量 %.2f GB[%.2f GB],上传速度 %.2f MB/s,下载速度 %.2f MB/s.\n",
		s.Remark, float64(s.Status.FreeSpaceOnDisk)/1073741824, float64(s.Status.EstimatedQuota)/1073741824.0,
		float64(s.Status.UpInfoSpeed)/1048576.0, float64(s.Status.DownInfoSpeed)/1048576.0,
	)
}

func NewClientWrapper(baseURL string, username string, password string, remark string, rule config.ServerRule) Server {
	var logger = logrus.New()
	server := NewClient(baseURL, logger)
	server.Login(username, password)

	return Server{
		Client: server,
		Rule:   rule,
		Remark: remark,
	}
}