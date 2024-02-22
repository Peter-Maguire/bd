import { Component, ErrorInfo, ReactNode } from 'react';
import Typography from '@mui/material/Typography';
import { logError } from '../util';

interface BoundaryState {
    hasError: boolean;
}

interface BoundaryProps {
    children: ReactNode;
}

export class ErrorBoundary extends Component<BoundaryProps, BoundaryState> {
    constructor(props: BoundaryProps) {
        super(props);
        this.state = { hasError: false };
    }

    static getDerivedStateFromError() {
        return { hasError: true };
    }

    componentDidCatch(error: Error, errorInfo: ErrorInfo) {
        // TODO record somewhere
        logError(error);
        logError(errorInfo);
    }

    render(): ReactNode {
        if (this.state.hasError) {
            if (import.meta.env.PROD) {
                setInterval(() => window.location.reload(), 5000);
            }

            return (
                <Typography
                    marginTop={3}
                    variant={'h2'}
                    color={'error'}
                    textAlign={'center'}
                >
                    🤯 🤯 🤯 Something went wrong, reloading in 5 seconds... 🤯
                    🤯 🤯
                </Typography>
            );
        }
        return this.props.children;
    }
}
