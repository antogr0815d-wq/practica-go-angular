import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';

export interface Tarea {
  id: number;
  titulo: string;
  completada: boolean;
}

@Injectable({ providedIn: 'root' })
export class TareasService {
  private readonly http = inject(HttpClient);
  private readonly url = '/tareas';

  listar() {
    return this.http.get<Tarea[]>(this.url);
  }

  crear(titulo: string) {
    return this.http.post<Tarea>(this.url, { titulo, completada: false });
  }

  actualizar({ id, titulo, completada }: Tarea) {
    return this.http.put<Tarea>(this.url, { titulo, completada }, { params: { id } });
  }

  eliminar(id: number) {
    return this.http.delete<void>(this.url, { params: { id } });
  }
}
