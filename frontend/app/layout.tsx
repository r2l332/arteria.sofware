import type { Metadata } from 'next';
import './globals.css';
import { AuthGate } from '@/components/AuthGate';

export const metadata: Metadata = {
  title: 'Arteria — Integration Engine',
  description: 'High-throughput HL7v2/FHIR/DICOM interoperability engine',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className="antialiased">
        <AuthGate>{children}</AuthGate>
      </body>
    </html>
  );
}
