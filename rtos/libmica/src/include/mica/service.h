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
 * UMT 接收回调类型：数据到达时在库内线程中调用。
 * @param data     接收到的数据指针，仅在回调执行期间有效，返回后不可再使用
 * @param data_len 接收长度
 * @param priv     注册时传入的指针（如应用上下文），原样回传
 */
typedef void (*umt_rcv_cb_t)(const void *data, int data_len, void *priv);

/**
 * 注册 UMT 接收回调（库内线程等待数据并调用 callback）。
 * 仅支持一个回调；与 mica_rcv_data 互斥，不能同时使用。
 * @param callback  收到数据时调用 (data, data_len, priv)，data 仅在调用期间有效
 * @param priv      不透明指针，原样传给 callback（如应用上下文）
 * @return: 0 成功，-EALREADY 已注册，-EINVAL/-EAGAIN 等失败
 */
int mica_umt_register_rcv_cb(umt_rcv_cb_t callback, void *priv);

/**
 * 取消注册 UMT 接收回调。之后由 mica_rcv_data() 被动拉取。
 * @return: 0 成功
 */
int mica_umt_unregister_rcv_cb(void);

/**
 * 接收数据从对端（被动拉取，UMT拷贝到用户 buffer）。
 * 与回调模式互斥：已注册 mica_umt_register_rcv_cb 时调用返回 -EBUSY。
 * @param buffer: 接收缓冲区
 * @param len:    输入缓冲区大小，输出实际接收长度
 * @return: 0成功，负数失败（-EBUSY 表示当前为回调模式）
 */
int mica_rcv_data(void *buffer, size_t *len);

/**
 * 发送数据到对端（UMT零拷贝传输）
 * @param data: 数据缓冲区
 * @param offset: 数据缓冲区偏移量
 * @param len: 数据长度
 * @return: 0成功，负数失败
 */
 int mica_send_data(void *data, int offset, size_t len);


#ifdef __cplusplus
}
#endif

#endif /* MICA_SERVICE_H */