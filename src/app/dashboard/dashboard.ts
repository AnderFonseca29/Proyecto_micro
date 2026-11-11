import { ChangeDetectionStrategy, Component, OnInit, inject } from '@angular/core';
import { KpiSection } from './kpi-section/kpi-section';
import { StandsSection } from './stands-section/stands-section';
import { ManualControl } from './manual-control/manual-control';
import { AuthPanel } from './auth-panel/auth-panel';
import { AuditTable } from './audit-table/audit-table';
import { StoreService } from '../services/store.service';

/**
 * Dashboard — página principal de GESTOCK: dispara la carga inicial y
 * compone las secciones (KPIs → stands → control manual + auditoría).
 */
@Component({
  selector: 'app-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [KpiSection, StandsSection, AuthPanel, ManualControl, AuditTable],
  templateUrl: './dashboard.html',
  styleUrl: './dashboard.scss',
  host: { class: 'tema-beige' },
})
export class Dashboard implements OnInit {
  readonly store = inject(StoreService);

  ngOnInit(): void {
    void this.store.cargarInicial();
  }
}
