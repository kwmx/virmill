# Graceful VM reboot

Run `virmill vm reboot UUID --connection qemu:///system --plan` to review a
graceful reboot of a running VM. In the TUI, select **VMs → vm reboot** and enter
the stable UUID. Review the downtime, exact resource and acknowledgements before
applying the plan. The common plan/apply flow records intent before the request.

The native adapter subscribes to the selected domain's reboot event before
issuing one graceful request. It never falls back to reset or hard power-off.
Completion requires an observed event and a durable receipt tied to the plan and
operation. This verifies a reboot transition; guest login, services and networking
are separate readiness checks.

The event wait is bounded to 60 seconds. Native libvirt calls are synchronous and
their execution time is outside that event-wait bound. Missing events, lost
acknowledgements, disconnection or a coordinator crash before saving the receipt
leave the operation in recovery-required with its VM lock retained. Inspect it
using `virmill operation show ID` and `virmill operation reconcile ID`, also
available under **Jobs**. Reconciliation reads the saved receipt and never sends
another reboot. A running VM alone cannot prove that the request completed.

Coordinate external lifecycle administrators during the operation: libvirt events
identify the domain but do not include a request correlation ID. Disk and firmware
deletion are never part of this operation. Current generated tests exercise
failure and journal recovery; native guest reboot qualification remains required.
