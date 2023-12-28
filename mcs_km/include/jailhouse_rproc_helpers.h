/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: GPL-2.0
 */

#include <linux/remoteproc.h>
#include <linux/firmware.h>
#include <linux/elf.h>
#include <linux/namei.h>

#define JH_CELL_PATH	"/usr/share/jailhouse/cells"
#define JH_INMATE_PATH	"/lib/firmware"

int jh_cell_create_by_rproc(const struct rproc *rproc);
int jh_cell_load_by_rproc(const struct rproc *rproc, const struct firmware *fw);
int jh_cell_start_by_rproc(const struct rproc *rproc);
int jh_cell_stop_by_rproc(const struct rproc *rproc);
