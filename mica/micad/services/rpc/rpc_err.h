/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPC_ERR_H
#define RPC_ERR_H

#define RPC_EBASE        3000
#define RPC_ENO_SLOT     (RPC_EBASE + 1)
#define RPC_EOVERLONG    (RPC_EBASE + 2)
#define RPC_ECORRUPTED   (RPC_EBASE + 3)
#define RPC_ENEED_INIT   (RPC_EBASE + 4)
#define RPC_EINVAL       (RPC_EBASE + 5)
#define RPC_ENOMEM       (RPC_EBASE + 6)
#endif  /* RPC_ERR_H */
