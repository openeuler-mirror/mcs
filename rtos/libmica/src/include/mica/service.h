/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_SERVICE_H
#define MICA_SERVICE_H

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ========== Service类型 ========== */
enum mica_service_type {
    MICA_SERVICE_RPC = 0,
    MICA_SERVICE_TTY,
    MICA_SERVICE_UMT,
    MICA_SERVICE_MAX
};

/* ========== Service配置 ========== */

/**
 * 创建所有默认service（RPC + TTY + UMT）
 * 注意：会创建独立的接收线程
 * @return: 0成功，负数失败
 */
int mica_create_all_services(void);

/**
 * 创建已注册的service endpoint
 * @param type: service类型
 * @return: 0成功，负数失败
 */
int mica_create_service(enum mica_service_type type);

/**
 * 停止接收线程
 * 注意：会停止所有service的接收
 */
void mica_stop_receiver(void);

/**
 * 检查service是否就绪
 * @param type: service类型
 * @return: 1就绪，0未就绪
 */
int mica_service_is_ready(enum mica_service_type type);

/* ========== TTY API ========== */
/**
 * TTY发送数据
 * @param data: 数据缓冲区
 * @param len: 数据长度
 * @return: 发送字节数，负数表示失败
 */
int mica_tty_send(unsigned char *data, size_t len);

/**
 * 打印到TTY endpoint
 * @param format: printf格式字符串
 * @return: 打印字符数，失败返回-1
 */
int mica_tty_printf(const char *format, ...);


/* ========== UMT API ========== */
/**
 * 发送数据到对端（UMT零拷贝传输）
 * @param data: 数据缓冲区
 * @param offset: 数据缓冲区偏移量
 * @param len: 数据长度
 * @return: 0成功，负数失败
 */
int mica_send_data(void *data, int offset, size_t len);

/**
 * 接收数据从对端（UMT零拷贝传输）
 * @param buffer: 接收缓冲区
 * @param len: 输入缓冲区大小，输出实际接收长度
 * @return: 0成功，负数失败
 */
int mica_rcv_data(void *buffer, size_t *len);


#ifdef __cplusplus
}
#endif

#endif /* MICA_SERVICE_H */