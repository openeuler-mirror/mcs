/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <string.h>
#include <sys/mman.h>
#include <stdbool.h>
#include <errno.h>
#include "mica_elf_loader.h"

/*
 * @brief: check if the image is valid
 * @pqaram[in] hdr: the header of the image, virtual address
 * @return: 0 if valid, -1 if invalid
 */
static int is_image_valid(Elf64_Ehdr *hdr)
{
    // Check that the file starts with the magic ELF number
    // 0x7F followed by ELF(45 4c 46) in ASCII
    if (memcmp(hdr->e_ident, ELFMAG, SELFMAG) != 0)
        return -1;

    return 0;
}

void *elf_image_load(char *elf_start, int mcs_fd, char *dst_p_addr)
{
    Elf64_Ehdr      *hdr;
    Elf64_Phdr      *phdr;
    Elf64_Shdr      *shdr;
    char            *start;
    char            *taddr;
    char            *base_addr;
    void            *e_entry;
    char            *dst_seg_v_addr;
    int i = 0;

    hdr = (Elf64_Ehdr *) elf_start;
    
    if (is_image_valid(hdr)) {
        printf("Invalid ELF image\n");
        errno = 0;
        return NULL;
    }

    // Entries in the program header table
    phdr = (Elf64_Phdr *)(elf_start + hdr->e_phoff);

    // Go over all the entries in the ELF
    for (i = 0; i < hdr->e_phnum; ++i) {
        if (phdr[i].p_type != PT_LOAD)
            continue;

        if (phdr[i].p_filesz > phdr[i].p_memsz) {
            printf("image_load:: p_filesz > p_memsz\n");
            errno = EINVAL;
            return NULL;
        }

        if (!phdr[i].p_filesz)
            continue;

        if (i == 0)
            base_addr = (char *)phdr[i].p_paddr;

        /* the address in the shared memory to put each program segment
         * use the physical address of each program segment to calculate the offset
         * because no matter where the program segment is loaded,
         * the comparative location of each program segment in the shared memory is the same
         */
        dst_seg_v_addr = mmap(NULL, phdr[i].p_filesz, PROT_READ | PROT_WRITE, MAP_SHARED,
                        mcs_fd, (off_t)dst_p_addr + (off_t)phdr[i].p_paddr - (off_t)base_addr);
        if (dst_seg_v_addr == MAP_FAILED) {
            printf("mmap reserved mem failed: dst_seg_v_addr:%p\n",
                    dst_seg_v_addr);
            return NULL;
        }
        
        // the beginning virtual address of each program segment
        start = elf_start + phdr[i].p_offset;
        memmove(dst_seg_v_addr, start, phdr[i].p_filesz);
        munmap(dst_seg_v_addr, phdr[i].p_filesz);
    }

    // return the physical address of the entry point of the ELF
    e_entry = (char *)((uintptr_t)hdr->e_entry - (uintptr_t)base_addr + (uintptr_t)dst_p_addr);
    printf("image_load:: e_entry: %p\n", e_entry);

    return e_entry;
}