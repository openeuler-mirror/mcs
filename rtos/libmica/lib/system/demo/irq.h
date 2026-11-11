/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_DEMO_IRQ_H
#define MICA_DEMO_IRQ_H
#include <demo_irq.h>
static inline int mica_request_irq(unsigned irq, mica_irq_handler_t fn, void *arg)
{
    return demo_request_irq(irq, fn, arg);
}
static inline int mica_unmask_irq(unsigned irq)
{
    return demo_irq_unmask(irq);
}
static inline int mica_trigger_irq(unsigned irq)
{
    return demo_irq_trigger(irq);
}
static inline int mica_sync_release_irq(unsigned irq, mica_deadline_t deadline)
{
    return demo_irq_sync_release(irq, deadline);
}
#endif
