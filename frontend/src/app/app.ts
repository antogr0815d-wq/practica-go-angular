import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';
import { Tarea, TareasService } from './tareas.service';

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule],
  selector: 'app-root',
  styleUrl: './app.css',
  templateUrl: './app.html',
})
export class App implements OnInit {
  private readonly api = inject(TareasService);

  protected readonly tareas = signal<Tarea[]>([]);
  protected readonly titulo = signal('');
  protected readonly cargando = signal(false);
  protected readonly guardando = signal(false);
  protected readonly errorCarga = signal(false);
  protected readonly errorOperacion = signal('');
  protected readonly ocupadas = signal(new Set<number>());

  protected readonly completadas = computed(() => this.tareas().filter((tarea) => tarea.completada).length);
  protected readonly progreso = computed(() => {
    const total = this.tareas().length;
    return total === 0 ? 0 : Math.round((this.completadas() / total) * 100);
  });

  ngOnInit() {
    void this.cargar();
  }

  protected async cargar() {
    this.cargando.set(true);
    this.errorCarga.set(false);
    try {
      this.tareas.set(await firstValueFrom(this.api.listar()));
    } catch {
      this.errorCarga.set(true);
    } finally {
      this.cargando.set(false);
    }
  }

  protected async crear() {
    const titulo = this.titulo().trim();
    if (!titulo || this.guardando() || this.cargando() || this.errorCarga()) return;

    this.guardando.set(true);
    this.errorOperacion.set('');
    try {
      const nueva = await firstValueFrom(this.api.crear(titulo));
      this.tareas.update((tareas) => [...tareas, nueva]);
      this.titulo.set('');
    } catch {
      this.errorOperacion.set('No se pudo guardar la tarea. Inténtalo de nuevo.');
    } finally {
      this.guardando.set(false);
    }
  }

  protected async cambiarEstado(tarea: Tarea, completada: boolean) {
    if (this.ocupadas().has(tarea.id)) return;

    this.errorOperacion.set('');
    this.ocupadas.update((ids) => new Set(ids).add(tarea.id));
    const cambio = { ...tarea, completada };
    this.reemplazar(cambio);
    try {
      this.reemplazar(await firstValueFrom(this.api.actualizar(cambio)));
    } catch {
      this.reemplazar(tarea);
      this.errorOperacion.set('No se pudo cambiar el estado de la tarea. Inténtalo de nuevo.');
    } finally {
      this.liberar(tarea.id);
    }
  }

  protected async eliminar(tarea: Tarea) {
    if (this.ocupadas().has(tarea.id)) return;

    this.errorOperacion.set('');
    this.ocupadas.update((ids) => new Set(ids).add(tarea.id));
    try {
      await firstValueFrom(this.api.eliminar(tarea.id));
      this.tareas.update((tareas) => tareas.filter(({ id }) => id !== tarea.id));
    } catch {
      this.errorOperacion.set('No se pudo eliminar la tarea. Inténtalo de nuevo.');
    } finally {
      this.liberar(tarea.id);
    }
  }

  private reemplazar(tarea: Tarea) {
    this.tareas.update((tareas) => tareas.map((actual) => (actual.id === tarea.id ? tarea : actual)));
  }

  private liberar(id: number) {
    this.ocupadas.update((ids) => {
      const pendientes = new Set(ids);
      pendientes.delete(id);
      return pendientes;
    });
  }
}
