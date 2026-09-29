import { Component, type ErrorInfo, type ReactNode } from "react";

import { reportError } from "@/shared/lib/monitor";
import { StandaloneLayout } from "@/shared/ui/StandaloneLayout";
import { StatusPage, type PageStatus } from "@/shared/ui/StatusPage";

export function NotFound() {
  return <StatusPage status={404} />;
}

export function Forbidden() {
  return <StatusPage status={403} />;
}

export function StandaloneErrorPage({ status }: { status: PageStatus }) {
  return (
    <StandaloneLayout>
      <StatusPage
        status={status}
        onRetry={status >= 500 ? () => window.location.reload() : undefined}
      />
    </StandaloneLayout>
  );
}

interface BoundaryState {
  failed: boolean;
  address?: string;
}

/** Смена адреса гасит ошибку без перемонтирования: иначе терялось бы состояние радара. */
export class ErrorBoundary extends Component<
  { children: ReactNode; address?: string; standalone?: boolean },
  BoundaryState
> {
  state: BoundaryState = { failed: false };

  static getDerivedStateFromError(): BoundaryState {
    return { failed: true };
  }

  static getDerivedStateFromProps(
    props: { address?: string },
    state: BoundaryState,
  ): BoundaryState | null {
    return props.address === state.address ? null : { failed: false, address: props.address };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("unhandled UI error", error, info.componentStack);
    reportError("render", error, { componentStack: (info.componentStack ?? "").slice(0, 2000) });
  }

  render() {
    if (this.state.failed) {
      const error = <StatusPage status={500} onRetry={() => window.location.reload()} />;
      return this.props.standalone ? <StandaloneLayout>{error}</StandaloneLayout> : error;
    }
    return this.props.children;
  }
}
