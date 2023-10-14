/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_ELF_LOADER_H
#define MICA_ELF_LOADER_H

#include <elf.h>

/*
 * @brief: load the image to the destination address
 * @param[in] elf_start: the start virtual address of the image file
 * @param[in] mcs_fd: the file descriptor of the mcs device
 * @param[in] dst_p_addr: the physical address of the destination memory area
 * @param[in] size: the shared memory size
 * @return: the physical address of the entry point to the image
 */
void* elf_image_load(char *elf_start, int mcs_fd, char *dst_p_addr);

#endif /* MICA_ELF_LOADER_H */