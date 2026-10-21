import { Component, type ErrorInfo, type ReactNode } from "react"
import { EmptyState } from "@/components/EmptyState"
import { Button } from "@/components/ui/button"

interface ErrorBoundaryProps {
  children: ReactNode
}

interface ErrorBoundaryState {
  failed: boolean
}

/**
 * Catches a render error of a page, so the sidebar keeps working and the user can try
 * again. It shows no error text: the message could hold data from the server.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { failed: false }

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("page crashed", error, info.componentStack)
  }

  render() {
    if (!this.state.failed) return this.props.children
    return (
      <EmptyState
        title="Something went wrong"
        text="This page failed to render."
        action={<Button onClick={() => this.setState({ failed: false })}>Try again</Button>}
      />
    )
  }
}
