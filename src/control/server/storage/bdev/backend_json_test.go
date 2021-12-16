//
// (C) Copyright 2021 Intel Corporation.
//
// SPDX-License-Identifier: BSD-2-Clause-Patent
//

package bdev

import (
	"fmt"
	"os"
	"testing"

	"github.com/dustin/go-humanize"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/daos-stack/daos/src/control/common"
	"github.com/daos-stack/daos/src/control/logging"
	"github.com/daos-stack/daos/src/control/server/engine"
	"github.com/daos-stack/daos/src/control/server/storage"
)

// TestBackend_newSpdkConfig verifies config parameters for bdev get
// converted into config content that can be consumed by spdk.
func TestBackend_newSpdkConfig(t *testing.T) {
	mockMntpt := "/mock/mnt/daos"
	tierID := 84
	host, _ := os.Hostname()

	tests := map[string]struct {
		class              storage.Class
		fileSizeGB         int
		devList            []string
		enableVmd          bool
		vosEnv             string
		expExtraBdevCfgs   []*SpdkSubsystemConfig
		expExtraSubsystems []*SpdkSubsystem
		expValidateErr     error
		expErr             error
	}{
		"config validation failure": {
			class:          storage.ClassNvme,
			devList:        []string{"not a pci address"},
			expValidateErr: errors.New("unexpected pci address"),
		},
		"multiple controllers": {
			class:   storage.ClassNvme,
			devList: []string{common.MockPCIAddr(1), common.MockPCIAddr(2)},
			expExtraBdevCfgs: []*SpdkSubsystemConfig{
				{
					Method: SpdkBdevNvmeAttachController,
					Params: NvmeAttachControllerParams{
						TransportType:    "PCIe",
						DeviceName:       fmt.Sprintf("Nvme_%s_0_%d", host, tierID),
						TransportAddress: common.MockPCIAddr(1),
					},
				},
				{
					Method: SpdkBdevNvmeAttachController,
					Params: NvmeAttachControllerParams{
						TransportType:    "PCIe",
						DeviceName:       fmt.Sprintf("Nvme_%s_1_%d", host, tierID),
						TransportAddress: common.MockPCIAddr(2),
					},
				},
			},
			vosEnv: "NVME",
		},
		"multiple controllers; vmd enabled": {
			class:     storage.ClassNvme,
			enableVmd: true,
			devList:   []string{common.MockPCIAddr(1), common.MockPCIAddr(2)},
			expExtraBdevCfgs: []*SpdkSubsystemConfig{
				{
					Method: SpdkBdevNvmeAttachController,
					Params: NvmeAttachControllerParams{
						TransportType:    "PCIe",
						DeviceName:       fmt.Sprintf("Nvme_%s_0_%d", host, tierID),
						TransportAddress: common.MockPCIAddr(1),
					},
				},
				{
					Method: SpdkBdevNvmeAttachController,
					Params: NvmeAttachControllerParams{
						TransportType:    "PCIe",
						DeviceName:       fmt.Sprintf("Nvme_%s_1_%d", host, tierID),
						TransportAddress: common.MockPCIAddr(2),
					},
				},
			},
			expExtraSubsystems: []*SpdkSubsystem{
				{
					Name: "vmd",
					Configs: []*SpdkSubsystemConfig{
						{
							Method: SpdkVmdEnable,
							Params: VmdEnableParams{},
						},
					},
				},
			},
			vosEnv: "NVME",
		},
		"AIO file class; multiple files; zero file size": {
			class:          storage.ClassFile,
			devList:        []string{"/path/to/myfile", "/path/to/myotherfile"},
			expValidateErr: errors.New("requires non-zero bdev_size"),
		},
		"AIO file class; multiple files; non-zero file size": {
			class:      storage.ClassFile,
			fileSizeGB: 1,
			devList:    []string{"/path/to/myfile", "/path/to/myotherfile"},
			expExtraBdevCfgs: []*SpdkSubsystemConfig{
				{
					Method: SpdkBdevAioCreate,
					Params: AioCreateParams{
						BlockSize:  humanize.KiByte * 4,
						DeviceName: fmt.Sprintf("AIO_%s_0_%d", host, tierID),
						Filename:   "/path/to/myfile",
					},
				},
				{
					Method: SpdkBdevAioCreate,
					Params: AioCreateParams{
						BlockSize:  humanize.KiByte * 4,
						DeviceName: fmt.Sprintf("AIO_%s_1_%d", host, tierID),
						Filename:   "/path/to/myotherfile",
					},
				},
			},
			vosEnv: "AIO",
		},
		"AIO kdev class; multiple devices": {
			class:   storage.ClassKdev,
			devList: []string{"/dev/sdb", "/dev/sdc"},
			expExtraBdevCfgs: []*SpdkSubsystemConfig{
				{
					Method: SpdkBdevAioCreate,
					Params: AioCreateParams{
						DeviceName: fmt.Sprintf("AIO_%s_0_%d", host, tierID),
						Filename:   "/dev/sdb",
					},
				},
				{
					Method: SpdkBdevAioCreate,
					Params: AioCreateParams{
						DeviceName: fmt.Sprintf("AIO_%s_1_%d", host, tierID),
						Filename:   "/dev/sdc",
					},
				},
			},
			vosEnv: "AIO",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			log, buf := logging.NewTestLogger(t.Name())
			defer common.ShowBufferOnFailure(t, buf)

			cfg := &storage.TierConfig{
				Tier:  tierID,
				Class: storage.ClassNvme,
				Bdev: storage.BdevConfig{
					DeviceList: tc.devList,
					FileSize:   tc.fileSizeGB,
				},
			}
			if tc.class != "" {
				cfg.Class = tc.class
			}

			engineConfig := engine.NewConfig().
				WithFabricProvider("test"). // valid enough to pass "not-blank" test
				WithFabricInterface("test").
				WithFabricInterfacePort(42).
				WithStorage(
					storage.NewTierConfig().
						WithScmClass("dcpm").
						WithScmDeviceList("foo").
						WithScmMountPoint(mockMntpt),
					cfg,
				)

			gotValidateErr := engineConfig.Validate() // populate output path
			common.CmpErr(t, tc.expValidateErr, gotValidateErr)
			if tc.expValidateErr != nil {
				return
			}

			writeReq, _ := storage.BdevWriteConfigRequestFromConfig(log, &engineConfig.Storage)
			if tc.enableVmd {
				writeReq.VMDEnabled = true
			}

			gotCfg, gotErr := newSpdkConfig(log, &writeReq)
			common.CmpErr(t, tc.expErr, gotErr)
			if tc.expErr != nil {
				return
			}

			expCfg := defaultSpdkConfig()
			expCfg.Subsystems[0].Configs = append(expCfg.Subsystems[0].Configs, tc.expExtraBdevCfgs...)
			expCfg.Subsystems = append(expCfg.Subsystems, tc.expExtraSubsystems...)

			if diff := cmp.Diff(expCfg, gotCfg); diff != "" {
				t.Fatalf("(-want, +got):\n%s", diff)
			}

			if engineConfig.Storage.VosEnv != tc.vosEnv {
				t.Fatalf("expected VosEnv to be %q, but it was %q", tc.vosEnv, engineConfig.Storage.VosEnv)
			}
		})
	}
}
