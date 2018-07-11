/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package app

import (
	"context"
	"fmt"
	"os"

	"configcenter/src/apimachinery"
	"configcenter/src/apimachinery/util"
	"configcenter/src/common/backbone"
	cc "configcenter/src/common/backbone/configcenter"
	"configcenter/src/common/rdapi"
	"configcenter/src/common/types"
	"configcenter/src/common/version"
	"configcenter/src/source_controller/hostcontroller/app/options"
	"configcenter/src/source_controller/hostcontroller/logics"
	"configcenter/src/source_controller/hostcontroller/service"
	"configcenter/src/storage"
	"configcenter/src/storage/mgoclient"
	"configcenter/src/storage/redisclient"
	"github.com/emicklei/go-restful"
)

//Run ccapi server
func Run(ctx context.Context, op *options.ServerOption) error {
	svrInfo, err := newServerInfo(op)
	if err != nil {
		return fmt.Errorf("wrap server info failed, err: %v", err)
	}

	c := &util.APIMachineryConfig{
		ZkAddr:    op.ServConf.RegDiscover,
		QPS:       1000,
		Burst:     2000,
		TLSConfig: nil,
	}

	machinery, err := apimachinery.NewApiMachinery(c)
	if err != nil {
		return fmt.Errorf("new api machinery failed, err: %v", err)
	}

	coreService := new(service.Service)
	server := backbone.Server{
		ListenAddr: svrInfo.IP,
		ListenPort: svrInfo.Port,
		Handler:    restful.NewContainer().Add(coreService.WebService()),
		TLS:        backbone.TLSConfig{},
	}

	regPath := fmt.Sprintf("%s/%s/%s", types.CC_SERV_BASEPATH, types.CC_MODULE_HOST, svrInfo.IP)
	bonC := &backbone.Config{
		RegisterPath: regPath,
		RegisterInfo: *svrInfo,
		CoreAPI:      machinery,
		Server:       server,
	}

	hostCtrl := new(HostController)
	hostCtrl.Core, err = backbone.NewBackbone(ctx, op.ServConf.RegDiscover,
		types.CC_MODULE_HOST,
		op.ServConf.ExConfig,
		hostCtrl.onHostConfigUpdate,
		bonC)
	if err != nil {
		return fmt.Errorf("new backbone failed, err: %v", err)
	}

	mgc := hostCtrl.Config.Mongo
	hostCtrl.Instance, err = mgoclient.NewMgoCli(mgc.Address, mgc.Port, mgc.User, mgc.Password, mgc.Mechanism, mgc.Database)
	if err != nil {
		return fmt.Errorf("new mongo client failed, err: %v", err)
	}

	rdsc := hostCtrl.Config.Redis
	hostCtrl.Cache, err = redisclient.NewRedis(rdsc.Address, rdsc.Port, rdsc.User, rdsc.Password, rdsc.Database)
	if err != nil {
		return fmt.Errorf("new redis client failed, err: %v", err)
	}

	coreService.Core = hostCtrl.Core
	coreService.Instance = hostCtrl.Instance
	coreService.Cache = hostCtrl.Cache
	coreService.Logics = logics.Logics{Instance: hostCtrl.Instance}

	select {}
	return nil
}

type HostController struct {
	Core     *backbone.Engine
	Instance storage.DI
	Cache    storage.DI
	Config   options.Config
}

func (h *HostController) onHostConfigUpdate(previous, current cc.ProcessConfig) {
	prefix := storage.DI_MONGO
	h.Config.Mongo = mgoclient.MongoConfig{
		Address:      current.ConfigMap[prefix+".host"],
		User:         current.ConfigMap[prefix+".user"],
		Password:     current.ConfigMap[prefix+".pwd"],
		Database:     current.ConfigMap[prefix+".database"],
		Port:         current.ConfigMap[prefix+".port"],
		MaxOpenConns: current.ConfigMap[prefix+".maxOpenConns"],
		MaxIdleConns: current.ConfigMap[prefix+".maxIDleConns"],
		Mechanism:    current.ConfigMap[prefix+".mechanism"],
	}

	prefix = storage.DI_REDIS
	h.Config.Redis = redisclient.RedisConfig{
		Address:  current.ConfigMap[prefix+".host"],
		User:     current.ConfigMap[prefix+".user"],
		Password: current.ConfigMap[prefix+".pwd"],
		Database: current.ConfigMap[prefix+".database"],
		Port:     current.ConfigMap[prefix+".port"],
	}
}

func newServerInfo(op *options.ServerOption) (*types.ServerInfo, error) {
	ip, err := op.ServConf.GetAddress()
	if err != nil {
		return nil, err
	}

	port, err := op.ServConf.GetPort()
	if err != nil {
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	info := &types.ServerInfo{
		IP:       ip,
		Port:     port,
		HostName: hostname,
		Scheme:   "http",
		Version:  version.GetVersion(),
		Pid:      os.Getpid(),
	}
	return info, nil
}
