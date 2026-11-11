/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef __MICA_IRQ__H__
#define __MICA_IRQ__H__
#include <mica/platform/time.h>

typedef void (*mica_irq_handler_t)(void *);
/* request registers only: failure adds no registration and never removes
 * another owner's registration. On success record ownership before unmask.
 * sync_release failure retains the registration and callback argument. */
#include <mica/platform/system/@PROJECT_SYSTEM@/irq.h>
#endif
